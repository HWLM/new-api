import json, io, collections

NEW = {
 'By Day': {
   'en':'By Day','zh':'按天','zh-TW':'按天',
   'fr':'Par jour','ru':'По дням','ja':'日別','vi':'Theo ngày'},
 'By Month': {
   'en':'By Month','zh':'按月','zh-TW':'按月',
   'fr':'Par mois','ru':'По месяцам','ja':'月別','vi':'Theo tháng'},
 'Amount (¥)': {
   'en':'Amount (¥)','zh':'金额（¥）','zh-TW':'金額（¥）',
   'fr':'Montant (¥)','ru':'Сумма (¥)','ja':'金額（¥）','vi':'Số tiền (¥)'},
}

for loc in ['en','zh','zh-TW','fr','ru','ja','vi']:
    p = 'src/i18n/locales/%s.json' % loc
    data = json.loads(io.open(p, encoding='utf-8').read(),
                      object_pairs_hook=collections.OrderedDict)
    tr = data['translation']
    added = 0
    for k, v in NEW.items():
        if k not in tr:
            tr[k] = v[loc]
            added += 1
    out = json.dumps(data, ensure_ascii=False, indent=2) + '\n'
    # 还原被刻意转义的受保护键名
    out = out.replace('"footer.newapi.projectAttributionSuffix":',
                      '"footer.new\\u0061pi.projectAttributionSuffix":')
    io.open(p, 'w', encoding='utf-8', newline='\n').write(out)
    print(loc, 'added', added, 'total', len(tr))
