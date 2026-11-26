// Representative chat scenarios. Used by the reference generator and fixture.
export const comparison = `root = Card(Tabs([{title:"Quiet studio",child:quiet},{title:"Fast workshop",child:fast}]))
quiet = Text("Quiet studio: 20 minutes away, $15, no tools included.")
fast = Text("Fast workshop: 10 minutes away, $30, tools included.")`;

export const collection = `$/items = [{name:"Notebooks",cost:12.5},{name:"Pens",cost:8}]
root = Card(List(_template($/items,item)))
item = Row([name,cost],justify="spaceBetween")
name = Text($name)
cost = Text(formatNumber($cost,2))`;

export const draft = `$/draft = {email:"missing-at",copies:2,notes:false,plan:[]}
root = Card(Column([email,copies,notes,plan,echo]))
email = TextField("Sample email",$/draft/email,checks=[?email("Enter an email address")])
copies = Slider(label="Copies",min=1,max=5,steps=4,value=$/draft/copies)
notes = CheckBox("Include notes",$/draft/notes)
plan = ChoicePicker(label="Detail",variant="mutuallyExclusive",options=[{label:"Brief",value:"brief"},{label:"Full",value:"full"}],value=$/draft/plan)
echo = Text($/draft/email)`;

export const exam = `$/answers = {version:[],platform:"",correct:[],logs:true,mail:false,confidence:70,notes:"内部备注"}
root = Form(child=questions,title="发布前小测")
questions = Column([version,platform,correct,details])
version = ChoicePicker(label="1. 哪个版本可以发布？",options=[{label:"候选版本",value:"candidate"},{label:"通过验收的版本",value:"verified"}],value=$/answers/version,checks=[?required("请选择一个版本。")])
platform = TextField("2. 填写待验证的平台",$/answers/platform,placeholder="例如 Windows",checks=[?required("请输入待验证的平台。")])
correct = ChoicePicker(label="3. 构建通过等于实机验收。",displayStyle="chips",options=[{label:"正确",value:"yes"},{label:"错误",value:"no"}],value=$/answers/correct,checks=[?required("请选择正确或错误。")])
details = Tabs([{title:"基本信息",child:basic},{title:"高级设置",child:advanced}])
basic = Column([Text("4. 附加记录", "title"),logs,mail,confidence])
logs = CheckBox("包含构建日志",$/answers/logs)
mail = CheckBox("发送邮件通知",$/answers/mail)
confidence = Slider(label="5. 验收信心",min=0,max=100,steps=10,value=$/answers/confidence)
advanced = TextField("内部备注",$/answers/notes)`;
