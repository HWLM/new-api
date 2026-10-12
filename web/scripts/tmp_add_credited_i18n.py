import json, io, collections

NEW = {
 'Credit Customer': {
   'en':'Credit Customer','zh':'授信客户','zh-TW':'授信客戶',
   'fr':'Client avec crédit','ru':'Клиент с кредитом','ja':'与信顧客','vi':'Khách hàng tín dụng'},
 'Credit Customer?': {
   'en':'Credit Customer?','zh':'是否授信客户','zh-TW':'是否授信客戶',
   'fr':'Client avec crédit ?','ru':'Клиент с кредитом?','ja':'与信顧客か','vi':'Khách hàng tín dụng?'},
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
    out = out.replace('"footer.newapi.projectAttributionSuffix":',
                      '"footer.new\\u0061pi.projectAttributionSuffix":')
    io.open(p, 'w', encoding='utf-8', newline='\n').write(out)
    print(loc, 'added', added, 'total', len(tr))
