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
