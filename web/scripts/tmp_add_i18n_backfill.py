import json, io, collections

NEW = {
 'Add Historical Record': {
   'en':'Add Historical Record','zh':'录入历史数据','zh-TW':'錄入歷史資料',
   'fr':'Ajouter un enregistrement historique','ru':'Добавить историческую запись',
   'ja':'過去データを登録','vi':'Nhập dữ liệu lịch sử'},
 'For credit and repayment that happened before this feature went live. Only records the entry — user quota is not changed.': {
   'en':'For credit and repayment that happened before this feature went live. Only records the entry — user quota is not changed.',
   'zh':'用于补录本功能上线前已发生的授信与回款。只登记流水，不会改动用户额度。',
   'zh-TW':'用於補錄本功能上線前已發生的授信與回款。只登記流水，不會改動使用者額度。',
   'fr':"Pour les credits et remboursements anterieurs a la mise en service. Enregistre uniquement l'ecriture, le quota utilisateur n'est pas modifie.",
   'ru':'Для кредитов и возвратов, произошедших до запуска функции. Записывается только проводка, квота пользователя не меняется.',
   'ja':'本機能の公開前に発生した与信・回収の補登録用です。記録のみで、ユーザーのクォータは変更されません。',
   'vi':'Dành cho tín dụng và thu hồi phát sinh trước khi tính năng này hoạt động. Chỉ ghi nhận bản ghi, không thay đổi hạn mức người dùng.'},
 'Date Occurred': {
   'en':'Date Occurred','zh':'发生日期','zh-TW':'發生日期',
   'fr':'Date de survenance','ru':'Дата события','ja':'発生日','vi':'Ngày phát sinh'},
 'Use the date the credit or repayment actually happened, otherwise the monthly report will be wrong.': {
   'en':'Use the date the credit or repayment actually happened, otherwise the monthly report will be wrong.',
   'zh':'请填写授信或回款实际发生的日期，否则月度明细的月初/月末未回款金额会算错。',
   'zh-TW':'請填寫授信或回款實際發生的日期，否則月度明細的月初/月末未回款金額會算錯。',
   'fr':'Indiquez la date reelle du credit ou du remboursement, sinon le rapport mensuel sera errone.',
   'ru':'Укажите фактическую дату кредита или возврата, иначе месячный отчёт будет неверным.',
   'ja':'与信・回収が実際に発生した日付を入力してください。そうしないと月次明細が正しく計算されません。',
   'vi':'Hãy nhập ngày tín dụng hoặc thu hồi thực tế phát sinh, nếu không báo cáo theo tháng sẽ sai.'},
 'Please select a user': {
   'en':'Please select a user','zh':'请选择用户','zh-TW':'請選擇使用者',
   'fr':'Veuillez selectionner un utilisateur','ru':'Выберите пользователя',
   'ja':'ユーザーを選択してください','vi':'Vui lòng chọn người dùng'},
 'Please enter a valid amount': {
   'en':'Please enter a valid amount','zh':'请输入有效的金额','zh-TW':'請輸入有效的金額',
   'fr':'Veuillez saisir un montant valide','ru':'Введите корректную сумму',
   'ja':'有効な金額を入力してください','vi':'Vui lòng nhập số tiền hợp lệ'},
 'Amount is too large': {
   'en':'Amount is too large','zh':'金额过大','zh-TW':'金額過大',
   'fr':'Le montant est trop eleve','ru':'Сумма слишком велика',
   'ja':'金額が大きすぎます','vi':'Số tiền quá lớn'},
 'Please select the date it occurred': {
   'en':'Please select the date it occurred','zh':'请选择发生日期','zh-TW':'請選擇發生日期',
   'fr':'Veuillez selectionner la date de survenance','ru':'Выберите дату события',
   'ja':'発生日を選択してください','vi':'Vui lòng chọn ngày phát sinh'},
 'Date cannot be in the future': {
   'en':'Date cannot be in the future','zh':'日期不能晚于今天','zh-TW':'日期不能晚於今天',
   'fr':'La date ne peut pas etre dans le futur','ru':'Дата не может быть в будущем',
   'ja':'日付に未来は指定できません','vi':'Ngày không thể ở tương lai'},
 'Historical record added': {
   'en':'Historical record added','zh':'历史数据录入成功','zh-TW':'歷史資料錄入成功',
   'fr':'Enregistrement historique ajoute','ru':'Историческая запись добавлена',
   'ja':'過去データを登録しました','vi':'Đã thêm dữ liệu lịch sử'},
 'Failed to add record': {
   'en':'Failed to add record','zh':'录入失败','zh-TW':'錄入失敗',
   'fr':"Echec de l'ajout de l'enregistrement",'ru':'Не удалось добавить запись',
   'ja':'登録に失敗しました','vi':'Thêm bản ghi thất bại'},
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
