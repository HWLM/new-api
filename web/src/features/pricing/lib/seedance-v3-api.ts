/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact support@quantumnous.com
*/

export const SEEDANCE_V3_ENDPOINT_TYPE = 'seedance-v3'
export const SEEDANCE_V3_ENDPOINT_PATH =
  '/api/v3/contents/generations/tasks'

export function isSeedanceV3ModelName(modelName: string): boolean {
  return /(?:^|-)seedance-2-0(?:-|$)/i.test(modelName)
}

export function isDoubaoSeedanceV3ModelName(modelName: string): boolean {
  return /^doubao-seedance-2-0(?:-|$)/i.test(modelName)
}

type SeedanceV3SampleLanguage =
  | 'curl'
  | 'python'
  | 'typescript'
  | 'javascript'

type SeedanceV3SampleContext = {
  baseUrl: string
  apiKeyEnv: string
  modelName: string
}

export function buildSeedanceV3Sample(
  lang: SeedanceV3SampleLanguage,
  ctx: SeedanceV3SampleContext
): string {
  const url = `${ctx.baseUrl}${SEEDANCE_V3_ENDPOINT_PATH}`
  const body = {
    model: ctx.modelName,
    content: [
      {
        type: 'text',
        text: 'Make the subject turn and wave at the camera.',
      },
      {
        type: 'image_url',
        role: 'first_frame',
        image_url: { url: 'https://example.com/first-frame.jpg' },
      },
    ],
    duration: 5,
    resolution: '720p',
    ratio: '16:9',
    generate_audio: true,
    watermark: false,
  }
  const bodyJson = JSON.stringify(body, null, 2)

  if (lang === 'curl') {
    return [
      `curl -X POST ${url} \\`,
      `  -H "Authorization: Bearer $${ctx.apiKeyEnv}" \\`,
      `  -H "Content-Type: application/json" \\`,
      `  -d '${bodyJson.replaceAll('\n', '\n     ')}'`,
    ].join('\n')
  }

  if (lang === 'python') {
    const pythonBody = [
      '{',
      `    "model": "${ctx.modelName}",`,
      '    "content": [',
      '        {',
      '            "type": "text",',
      '            "text": "Make the subject turn and wave at the camera.",',
      '        },',
      '        {',
      '            "type": "image_url",',
      '            "role": "first_frame",',
      '            "image_url": {"url": "https://example.com/first-frame.jpg"},',
      '        },',
      '    ],',
      '    "duration": 5,',
      '    "resolution": "720p",',
      '    "ratio": "16:9",',
      '    "generate_audio": True,',
      '    "watermark": False,',
      '}',
    ].join('\n')
    return [
      'import requests',
      '',
      `response = requests.post(`,
      `    "${url}",`,
      `    headers={`,
      `        "Authorization": "Bearer <YOUR_API_KEY>",`,
      `        "Content-Type": "application/json",`,
      `    },`,
      `    json=${pythonBody.replaceAll('\n', '\n    ')},`,
      ')',
      '',
      'response.raise_for_status()',
      'print(response.json())',
    ].join('\n')
  }

  return [
    `const response = await fetch('${url}', {`,
    `  method: 'POST',`,
    `  headers: {`,
    `    Authorization: \`Bearer \${process.env.${ctx.apiKeyEnv}}\`,`,
    `    'Content-Type': 'application/json',`,
    `  },`,
    `  body: JSON.stringify(${bodyJson}),`,
    `})`,
    '',
    `const data = await response.json()`,
    `console.log(data)`,
  ].join('\n')
}
