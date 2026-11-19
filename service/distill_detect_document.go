package service

import (
	"strings"

	"github.com/tidwall/gjson"
)

// 蒸馏检测的正文采样：把请求 body 裁成送给判定服务（Jev /v1/systemone）的
// state 文本。
//
// 迁移自 hw_gateway 的 `services/decision_model/document.rs`，规则逐条对齐：
//
//   - 保留：`system` / `instructions` / `developer` 字段，以及 messages[].role==user
//     和 input[].role==user 的文本；
//   - 丢弃：assistant / tool_result / function_call_output / 图片 / base64；
//   - 按出现顺序加 `[system prompt N]` / `[user message N]` 标签，末尾再加一个
//     `[current ask]` 指向最后一条 user 文本；
//   - 完全重复的段落去重（重试 / resume 时客户会重贴同样的 context）；
//   - 超长时按配置截断：默认头尾采样，而不是只取尾部。
//
// ⛔ 为什么必须头尾采样：蒸馏/批处理流量是「一份 JSON 装几百条任务」的形状，
// 表明用途的证据（rubric、参考答案、正负例说明）几乎全在头部。只取尾部会
// 让这类请求被判成普通对话 —— 这是 hw_gateway 上线后实测踩到的坑。

const distillTruncationMark = "\n[...truncated...]\n"

// distillDocument 从请求 body 抽取「调用方指令 + 用户轮次」拼成文本。
func distillDocument(body []byte) string {
	var parts []string
	seen := make(map[string]struct{})

	// ⛔ 去重 key 是**正文本身**，不是加了标签之后的字符串。若按标签后的文本
	// 去重，每一条的标签序号都不同，去重永远不会命中 —— 重试 / resume 时客户
	// 重贴同样的 context 就会把送上游的量翻倍。
	push := func(text, label string) {
		norm := strings.TrimSpace(text)
		if norm == "" {
			return
		}
		if _, dup := seen[norm]; dup {
			return
		}
		seen[norm] = struct{}{}
		if label != "" {
			norm = label + "\n" + norm
		}
		parts = append(parts, norm)
	}

	sysIdx := 0
	pushSystem := func(text string) {
		if strings.TrimSpace(text) == "" {
			return
		}
		sysIdx++
		push(text, "[system prompt "+itoa(sysIdx)+"]")
	}

	// Anthropic 的 system 可以是字符串或 [{type:"text",text:"..."}] 数组。
	if sys := gjson.GetBytes(body, "system"); sys.Exists() {
		if sys.Type == gjson.String {
			pushSystem(sys.String())
		} else if sys.IsArray() {
			for _, item := range sys.Array() {
				pushSystem(item.Get("text").String())
			}
		}
	}
	// OpenAI Responses 的等价字段。
	for _, field := range []string{"instructions", "developer"} {
		if v := gjson.GetBytes(body, field); v.Exists() && v.Type == gjson.String {
			pushSystem(v.String())
		}
	}

	userIdx := 0
	var lastUser string
	collectUserTurns := func(path string) {
		arr := gjson.GetBytes(body, path)
		if !arr.IsArray() {
			return
		}
		for _, msg := range arr.Array() {
			if msg.Get("role").String() != "user" {
				// assistant / tool_result / function_call_output 一律丢弃：
				// 模型自己的输出不是「用途证据」，带上只会稀释信号。
				continue
			}
			text := messageText(msg.Get("content"))
			norm := strings.TrimSpace(text)
			if norm == "" {
				continue
			}
			userIdx++
			lastUser = norm
			push(norm, "[user message "+itoa(userIdx)+"]")
		}
	}
	collectUserTurns("messages")
	collectUserTurns("input")

	if lastUser != "" {
		parts = append(parts, "[current ask]\n"+lastUser)
	}

	return strings.Join(parts, "\n\n")
}

// messageText 取一条 message 的文本；content 可能是字符串，也可能是
// [{type:"text",text:"..."}] 数组。非文本块（图片、base64）跳过。
func messageText(content gjson.Result) string {
	if !content.Exists() {
		return ""
	}
	if content.Type == gjson.String {
		return content.String()
	}
	if !content.IsArray() {
		return ""
	}
	var b strings.Builder
	for _, block := range content.Array() {
		if block.Get("type").String() != "text" {
			continue
		}
		if b.Len() > 0 {
			b.WriteString("\n")
		}
		b.WriteString(block.Get("text").String())
	}
	return b.String()
}

// distillSample 按字符数（不是字节）截断，head > 0 时头尾采样。
//
// 采样结果必须只取决于输入本身（不掺时间或随机量），否则同一份正文会算出
// 不同的去重哈希，重复打分、白花钱。
func distillSample(text string, maxChars, headChars int) string {
	if maxChars <= 0 {
		return ""
	}
	runes := []rune(text)
	if len(runes) <= maxChars {
		return text
	}
	if headChars <= 0 {
		return string(runes[len(runes)-maxChars:])
	}
	head := headChars
	if maxHead := maxChars * 3 / 4; head > maxHead {
		head = maxHead
	}
	if head > len(runes) {
		head = len(runes)
	}
	tail := maxChars - head
	if tail <= 0 {
		return string(runes[:head])
	}
	return string(runes[:head]) + distillTruncationMark + string(runes[len(runes)-tail:])
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[i:])
}
