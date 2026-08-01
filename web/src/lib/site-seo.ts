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
const META_DESCRIPTION_SELECTOR = 'meta[name="description"]'
const ANALYTICS_SCRIPT_ATTR = 'data-site-analytics-script'

function appendExecutableScript(source: HTMLScriptElement) {
  const script = document.createElement('script')
  Array.from(source.attributes).forEach((attr) => {
    script.setAttribute(attr.name, attr.value)
  })
  script.setAttribute(ANALYTICS_SCRIPT_ATTR, 'true')
  script.text = source.text || source.textContent || ''
  document.head.appendChild(script)
}

function ensureMetaDescriptionTag(): HTMLMetaElement | null {
  if (typeof document === 'undefined') return null
  let meta = document.querySelector(META_DESCRIPTION_SELECTOR) as
    | HTMLMetaElement
    | null
  if (meta) return meta
  meta = document.createElement('meta')
  meta.name = 'description'
  document.head.appendChild(meta)
  return meta
}

export function applyMetaDescriptionToDom(description?: string | null) {
  if (typeof document === 'undefined') return
  const meta = ensureMetaDescriptionTag()
  if (!meta) return

  const normalized = description?.trim() || ''
  if (!normalized) {
    meta.removeAttribute('content')
    return
  }
  meta.setAttribute('content', normalized)
}

export function applyAnalyticsScriptToDom(scriptContent?: string | null) {
  if (typeof document === 'undefined') return

  document
    .querySelectorAll(`script[${ANALYTICS_SCRIPT_ATTR}="true"]`)
    .forEach((node) => node.remove())

  const normalized = scriptContent?.trim() || ''
  if (!normalized) return

  if (normalized.includes('<script')) {
    const template = document.createElement('template')
    template.innerHTML = normalized
    const scripts = template.content.querySelectorAll('script')
    if (scripts.length > 0) {
      scripts.forEach((source) => appendExecutableScript(source))
      return
    }
  }

  const script = document.createElement('script')
  script.type = 'text/javascript'
  script.setAttribute(ANALYTICS_SCRIPT_ATTR, 'true')
  script.text = normalized
  document.head.appendChild(script)
}
