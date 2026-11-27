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

export const budget = `$/budget = {people:"3",room:"120",food:"60",travel:"30"}
root = Card(Column([Text("预算分摊", "title"), costs, result, send]))
costs = Row([left,right])
left = Column([people,room],weight=1)
right = Column([food,travel],weight=1)
people = TextField("人数",$/budget/people,variant="number",checks=[?numberInRange(1,"人数至少为 1")])
room = TextField("场地",$/budget/room,variant="number",checks=[?numberInRange(0,"请输入非负金额")])
food = TextField("餐费",$/budget/food,variant="number",checks=[?numberInRange(0,"请输入非负金额")])
travel = TextField("交通",$/budget/travel,variant="number",checks=[?numberInRange(0,"请输入非负金额")])
result = Row([total,share])
total = Column([Text("合计"),Text(display(sum([$/budget/room,$/budget/food,$/budget/travel]),"检查费用",decimals=2),"title")],weight=1)
share = Column([Text("人均"),Text(display(calculate("/",sum([$/budget/room,$/budget/food,$/budget/travel]),$/budget/people),"请输入有效人数及费用",decimals=2),"title")],weight=1)
send = Button(Text("请 Bot 优化预算"), variant="primary", enabled=and([compare("ge",$/budget/people,1),compare("ge",$/budget/room,0),compare("ge",$/budget/food,0),compare("ge",$/budget/travel,0)]), action=Event("submit",{"人数":$/budget/people,"场地":$/budget/room,"餐费":$/budget/food,"交通":$/budget/travel},userMessage="请优化 {人数} 人的预算，场地 {场地} 元，餐费 {餐费} 元，交通 {交通} 元。"))`;

export const calculator = `$/calc = {entry:0,left:0,op:"+",next:true}
$/digits = [1,2,3,4,5,6,7,8,9,0]
$/operators = [{label:"+",symbol:"+"},{label:"−",symbol:"-"},{label:"×",symbol:"*"},{label:"÷",symbol:"/"}]
root = Card(Column([Text("计算器","title"),screen,digits,operators,actions]))
screen = Text(display($/calc/entry,"无法计算，请输入新数字"),"title")
digits = List(_template($/digits,digit),direction="horizontal")
digit = Button(Text(display($,"0")),action=Event("set",{"/calc/entry":choose($/calc/next,$,calculate("+",calculate("*",$/calc/entry,10),$)),"/calc/next":false}))
operators = List(_template($/operators,operation),direction="horizontal")
operation = Button(Text($label),enabled=present($/calc/entry),action=Event("set",{"/calc/left":$/calc/entry,"/calc/op":$symbol,"/calc/next":true}))
actions = Row([clear,equals])
clear = Button(Text("清除"),action=Event("set",{"/calc/entry":0,"/calc/left":0,"/calc/op":"+","/calc/next":true}))
equals = Button(Text("="),variant="primary",enabled=not($/calc/next),action=Event("set",{"/calc/entry":calculate($/calc/op,$/calc/left,$/calc/entry),"/calc/next":true}))`;

export const weather = `$/weather = {city:"",period:""}
root = Card(Column([Text("天气查询","title"),city,period,send]))
city = TextField("城市",$/weather/city,placeholder="例如上海浦东",checks=[?required("请输入城市")])
period = TextField("日期范围",$/weather/period,placeholder="例如未来七天",checks=[?required("请输入日期范围")])
send = Button(Text("查天气"),variant="primary",enabled=and([present($/weather/city),present($/weather/period)]),action=Event("submit",{"城市":$/weather/city,"日期":$/weather/period},userMessage="查一下{城市}{日期}的天气"))`;

export const practice = `$/quiz = {first:[],second:[],instant:true,graded:false,total:0,savedFirst:[],savedSecond:[],note:""}
root = Card(Column([Text("跨页练习","title"),instant,pages,actions,graded,note]))
instant = CheckBox("随练随判",$/quiz/instant)
pages = Tabs([{title:"单选",child:first},{title:"多选",child:second}])
first = Column([one,feedbackOne])
one = ChoicePicker(label="3 × 4 等于多少？",options=[{label:"10",value:"10"},{label:"12",value:"12"}],value=$/quiz/first,checks=[?required("请选择一个答案")])
feedbackOne = Show(and([$/quiz/instant,present($/quiz/first)]),Text(choose(sameSet($/quiz/first,["12"]),"答对了","再试一次")))
second = Column([two,feedbackTwo])
two = ChoicePicker(label="哪些是质数？",variant="multipleSelection",options=[{label:"3",value:"3"},{label:"4",value:"4"},{label:"5",value:"5"}],value=$/quiz/second,checks=[?required("请至少选择一个答案")])
feedbackTwo = Show(and([$/quiz/instant,present($/quiz/second)]),Text(choose(sameSet($/quiz/second,["3","5"]),"答对了","再试一次")))
actions = Row([grade,send])
grade = Button(Text("本地评分"),enabled=and([present($/quiz/first),present($/quiz/second)]),action=Event("set",{"/quiz/total":sum([sameSet($/quiz/first,["12"]),sameSet($/quiz/second,["3","5"])]),"/quiz/savedFirst":$/quiz/first,"/quiz/savedSecond":$/quiz/second,"/quiz/graded":true}))
send = Button(Text("请 Bot 批改"),variant="primary",enabled=and([present($/quiz/first),present($/quiz/second)]),action=Event("submit",{"第一题":$/quiz/first,"第二题":$/quiz/second},userMessage="请批改这份练习。第一题选了 {第一题}，第二题选了 {第二题}。"))
graded = Show($/quiz/graded,Column([Text("上次本地评分","title"),scores]))
scores = Table(headers=["得分","第一题","第二题"],rows=[[$/quiz/total,$/quiz/savedFirst,$/quiz/savedSecond]])
note = TextField("个人备注",$/quiz/note)`;
