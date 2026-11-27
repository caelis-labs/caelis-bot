# Present results in desktop chat

Give the important facts, recommendation and next step in Markdown. Add one compact block when tabs, grouped records, a local draft or a short questionnaire makes the reply easier to use. Put each sentinel at column one on its own line, with blank lines separating the block from prose. Keep each block independent.

Use the controls for a task the user can do in the card. Give every input a meaningful label and bind it to an initialized local path. TextField produces text, including its number variant. Use `?required("…")` for required answers and `?numberInRange(0,"…")` for numeric text. Use vertical ChoicePicker rows for long labels, chips for short choices, and Slider steps for bounded numeric choices. Use Table for a small set of records with column headers.

Use `Button(Text("…"), action=Event("set", {"/path": value}))` for local actions. Give the button a label that names its effect. A set event updates one or more existing absolute paths on the same card. Each value is evaluated against the pre-click data, then all changes publish together. Declare initial values before binding the inputs. Use `Show(condition, child)` and a button's `enabled` to follow the same data. Tab changes preserve bindings. For calculated output, use `display(value,"…")` with a recovery message for invalid input such as division by zero.

To hand something to Bot, use a button with a specific call to action and `Event("submit", {city: $/city, period: $/period}, userMessage="Check the weather in {city} for {period}.")`. Each named placeholder must match a declared field. Field values can come from any tab and can be derived with a catalog function. Use `enabled=and([present($/city),present($/period)])` or relevant checks so all required answers are valid before sending. Declare only the values needed for this request, including when multiple tabs have other inputs or private notes. The user can keep editing after sending. Submitted values appear in history as snapshots. Do not add a sending disclaimer or a copy of the outgoing text to the card.

Use a plain chat reply path on channels without desktop cards. Use the existing approval or question workflow for explicit Runtime decisions. Do not request secrets or hidden values. Saved root Form replies still render.

Use the signatures below. Assign initial data with `$/path = value`. Use double-quoted, single-line strings, and up to 12 items per list or group. The host accepts one `root` tree of up to 40 components per block. Keep repeated templates shallow. Use only the set and submit button events. Omit surface directives, explicit updates, media and standalone calls.

## Desktop chat examples

### Compare details in tabs

```text
<a2ui>
root = Card(Tabs([{title:"Quiet studio",child:quiet},{title:"Fast workshop",child:fast}]))
quiet = Text("Quiet studio: 20 minutes away, $15, no tools included.")
fast = Text("Fast workshop: 10 minutes away, $30, tools included.")
</a2ui>
```

### Repeat supplied records

```text
<a2ui>
$/items = [{name:"Notebooks",cost:12.5},{name:"Pens",cost:8}]
root = Card(List(_template($/items,item)))
item = Row([name,cost],justify="spaceBetween")
name = Text($name)
cost = Text(formatNumber($cost,2))
</a2ui>
```

### Edit and validate a local draft

```text
<a2ui>
$/draft = {email:"missing-at",copies:2,notes:false,plan:[]}
root = Card(Column([email,copies,notes,plan,echo]))
email = TextField("Sample email",$/draft/email,checks=[?email("Enter an email address")])
copies = Slider(label="Copies",min=1,max=5,steps=4,value=$/draft/copies)
notes = CheckBox("Include notes",$/draft/notes)
plan = ChoicePicker(label="Detail",variant="mutuallyExclusive",options=[{label:"Brief",value:"brief"},{label:"Full",value:"full"}],value=$/draft/plan)
echo = Text($/draft/email)
</a2ui>
```

### Compare and send a budget

```text
<a2ui>
$/budget = {people:"3",room:"120",food:"60",travel:"30"}
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
send = Button(Text("请 Bot 优化预算"), variant="primary", enabled=and([compare("ge",$/budget/people,1),compare("ge",$/budget/room,0),compare("ge",$/budget/food,0),compare("ge",$/budget/travel,0)]), action=Event("submit",{"人数":$/budget/people,"场地":$/budget/room,"餐费":$/budget/food,"交通":$/budget/travel},userMessage="请优化 {人数} 人的预算，场地 {场地} 元，餐费 {餐费} 元，交通 {交通} 元。"))
</a2ui>
```

### Calculate locally with a keypad

```text
<a2ui>
$/calc = {entry:0,left:0,op:"+",next:true}
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
equals = Button(Text("="),variant="primary",enabled=not($/calc/next),action=Event("set",{"/calc/entry":calculate($/calc/op,$/calc/left,$/calc/entry),"/calc/next":true}))
</a2ui>
```

### Send a weather query

```text
<a2ui>
$/weather = {city:"",period:""}
root = Card(Column([Text("天气查询","title"),city,period,send]))
city = TextField("城市",$/weather/city,placeholder="例如上海浦东",checks=[?required("请输入城市")])
period = TextField("日期范围",$/weather/period,placeholder="例如未来七天",checks=[?required("请输入日期范围")])
send = Button(Text("查天气"),variant="primary",enabled=and([present($/weather/city),present($/weather/period)]),action=Event("submit",{"城市":$/weather/city,"日期":$/weather/period},userMessage="查一下{城市}{日期}的天气"))
</a2ui>
```

### Practice across tabs, score locally or ask Bot

```text
<a2ui>
$/quiz = {first:[],second:[],instant:true,graded:false,total:0,savedFirst:[],savedSecond:[],note:""}
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
note = TextField("个人备注",$/quiz/note)
</a2ui>
```

## Protocol and catalog reference

<!-- Generated by @a2ui/agent SkillGenerator from the desktop catalog derived from @a2ui/web_core Basic v1.0. Apache-2.0. Bot narrows the output contract and examples to the supported subset. As installed, the license is in ../assets/A2UI-LICENSE. -->

# A2UI Express DSL Output Contract

IMPORTANT: You MUST always surround the entire A2UI Express block with the literal sentinel tags `<a2ui>` and `</a2ui>` (always include both the opening and closing tags; do NOT wrap the tags in backticks or replace the opening tag with a markdown code fence such as ```a2ui).

The host compiler will compile your A2UI Express output into the correct JSON envelopes automatically.

## Grammar Rules

1. Component constructors can be assigned to variables or nested inline inside parent component arguments:
   header = ComponentA(prop1="val1")

   Keyword arguments (`param=value`) and positional arguments with `_` placeholders are supported.

   Variable names MUST start with a letter or underscore, and only contain letters, digits, and underscores.

2. The interface tree must have a single entry point assigned to the reserved variable 'root'.

3. Primitives:
   - Strings: Use double-quoted single-line strings.
   - Numbers: write as integers or decimals, e.g., 42
   - Booleans: write true or false
   - Null values: write null

4. Lists: represent as arrays, e.g., [child1, child2].

5. Maps: represent as key-value blocks, e.g., {title: "Overview", child: contentCol}. Map keys are always literal strings (dynamic variable resolution is not supported for keys).

6. Data bindings: prefix absolute paths in the data model with '$', e.g., $/user/firstName.
   Prefix relative list scopes with '$', e.g., $firstName.
   A lone '$' represents an empty relative path which resolves to the root of the current context (e.g. inside a template, representing the entire item itself).

7. Prefix supported client checks with ?. For example, ?email("Enter an email address").

9. Nested functions: call client functions directly using catalog signatures, for example myFunction("value").

10. Dynamic list templates: If a component expects a template child list, represent it using the _template helper:
    _template($/path/to/list, itemTemplate)
    And define the template component variable on another line, utilizing relative path references prefixed with $:
    itemTemplate = Text($name)

11. Static properties: Arguments annotated with '(static)' in the signatures below MUST be defined as literal values or arrays inline. You CANNOT use a dynamic data binding path (prefixed by $) for these arguments.

## System Functions

These functions are defined by the A2UI protocol, not by a catalog. Call them by their `@` name, like any function, and never add a `catalogId` to them:
• @index(offset?)
  - Description: Returns the 0-based index of the current item when rendering a dynamic list from a template. This function MUST ONLY be available when evaluating template items within a list context.
  - offset: Optional. An offset to add to the 0-based index (e.g., 1 for 1-based indexing). Defaults to 0.

## Positional Component Signatures

Use these exact positional signatures to instantiate components. Do not output property keys:
• Button(child (static), variant? (static), action (static), weight? (static), enabled?, checks? (static))
  - child: The ID of the child component. Use a 'Text' component for a labeled button. Only use an 'Icon' if the requirements explicitly ask for an icon-only button.
  - variant: A hint for the button style. If omitted, a default button style is used. 'primary' indicates this is the main call-to-action button. 'borderless' means the button has no visual border or background, making its child content appear like a clickable link. Must be one of: 'default', 'primary', 'borderless'
  - weight: The relative weight of this component within a Row or Column. This is similar to the CSS 'flex-grow' property. Note: this may ONLY be set when the component is a direct descendant of a Row or Column.
  - enabled: Allow this action only while this condition is true.
  - checks: A list of validation check rules to perform on the component.
• Card(child (static), weight? (static))
  - child: The ID of the single child component to be rendered inside the card. To display multiple elements, you MUST wrap them in a layout component (like Column or Row) and pass that container's ID here. Do NOT pass multiple IDs or a non-existent ID.
  - weight: The relative weight of this component within a Row or Column. This is similar to the CSS 'flex-grow' property. Note: this may ONLY be set when the component is a direct descendant of a Row or Column.
• CheckBox(label, value, weight? (static), checks? (static))
  - label: The text to display next to the checkbox.
  - value: The current state of the checkbox (true for checked, false for unchecked).
  - weight: The relative weight of this component within a Row or Column. This is similar to the CSS 'flex-grow' property. Note: this may ONLY be set when the component is a direct descendant of a Row or Column.
• ChoicePicker(label?, variant? (static), options (static), value, displayStyle? (static), filterable? (static), weight? (static), checks? (static))
  - Description: A component that allows selecting one or more options from a list.
  - label: The label for the group of options.
  - variant: A hint for how the choice picker should be displayed and behave. Must be one of: 'multipleSelection', 'mutuallyExclusive'
  - options: The list of available options to choose from.
    List of maps keys:
    * label - The text to display for this option.
    * value - The stable value associated with this option.
  - value: The list of currently selected values. This should be bound to a string array in the data model.
  - displayStyle: The display style of the component. Must be one of: 'checkbox', 'chips'
  - filterable: If true, displays a search input to filter the options.
  - weight: The relative weight of this component within a Row or Column. This is similar to the CSS 'flex-grow' property. Note: this may ONLY be set when the component is a direct descendant of a Row or Column.
• Column(children, justify? (static), align? (static), weight? (static))
  - Description: A layout component that arranges its children vertically. To create a grid layout, nest Rows within this Column.
  - children: Defines the children. Use an array of strings for a fixed set of children, or a template object to generate children from a data list. Children cannot be defined inline, they must be referred to by ID.
  - justify: Defines the arrangement of children along the main axis (vertically). Use 'spaceBetween' to push items to the edges (e.g. header at top, footer at bottom), or 'start'/'end'/'center' to pack them together. Must be one of: 'start', 'center', 'end', 'spaceBetween', 'spaceAround', 'spaceEvenly', 'stretch'
  - align: Defines the alignment of children along the cross axis (horizontally). This is similar to the CSS 'align-items' property. Must be one of: 'center', 'end', 'start', 'stretch'
  - weight: The relative weight of this component within a Row or Column. This is similar to the CSS 'flex-grow' property. Note: this may ONLY be set when the component is a direct descendant of a Row or Column.
• Divider(axis? (static), weight? (static))
  - axis: The orientation of the divider. Must be one of: 'horizontal', 'vertical'
  - weight: The relative weight of this component within a Row or Column. This is similar to the CSS 'flex-grow' property. Note: this may ONLY be set when the component is a direct descendant of a Row or Column.
• Form(child (static), title (static))
  - child: The ID of the single child component to be rendered inside the card. To display multiple elements, you MUST wrap them in a layout component (like Column or Row) and pass that container's ID here. Do NOT pass multiple IDs or a non-existent ID.
  - title: The title of the answer form.
• List(children, direction? (static), align? (static), weight? (static))
  - children: Defines the children. Use an array of strings for a fixed set of children, or a template object to generate children from a data list.
  - direction: The direction in which the list items are laid out. Must be one of: 'vertical', 'horizontal'
  - align: Defines the alignment of children along the cross axis. Must be one of: 'start', 'center', 'end', 'stretch'
  - weight: The relative weight of this component within a Row or Column. This is similar to the CSS 'flex-grow' property. Note: this may ONLY be set when the component is a direct descendant of a Row or Column.
• Row(children, justify? (static), align? (static), weight? (static))
  - Description: A layout component that arranges its children horizontally. To create a grid layout, nest Columns within this Row.
  - children: Defines the children. Use an array of strings for a fixed set of children, or a template object to generate children from a data list. Children cannot be defined inline, they must be referred to by ID.
  - justify: Defines the arrangement of children along the main axis (horizontally). Use 'spaceBetween' to push items to the edges, or 'start'/'end'/'center' to pack them together. Must be one of: 'center', 'end', 'spaceAround', 'spaceBetween', 'spaceEvenly', 'start', 'stretch'
  - align: Defines the alignment of children along the cross axis (vertically). This is similar to the CSS 'align-items' property, but uses camelCase values (e.g., 'start'). Must be one of: 'start', 'center', 'end', 'stretch'
  - weight: The relative weight of this component within a Row or Column. This is similar to the CSS 'flex-grow' property. Note: this may ONLY be set when the component is a direct descendant of a Row or Column.
• Show(when, child (static))
  - when: Show the child only while this condition is true.
• Slider(label?, min? (static), max (static), value, steps? (static), weight? (static), checks? (static))
  - label: The label for the slider.
  - min: The minimum value of the slider.
  - max: The maximum value of the slider.
  - value: The current value of the slider.
  - steps: The number of discrete divisions in the slider range. If specified, the slider will snap to discrete values.
  - weight: The relative weight of this component within a Row or Column. This is similar to the CSS 'flex-grow' property. Note: this may ONLY be set when the component is a direct descendant of a Row or Column.
• Table(headers (static), rows)
  - rows: Rows of cell values in header order.
• Tabs(tabs (static), weight? (static))
  - tabs: An array of objects, where each object defines a tab with a title and a child component.
    List of maps keys:
    * title - The tab title.
    * child - The ID of the child component.
  - weight: The relative weight of this component within a Row or Column. This is similar to the CSS 'flex-grow' property. Note: this may ONLY be set when the component is a direct descendant of a Row or Column.
• Text(text, variant? (static), weight? (static))
  - text: The text content to display. While simple Markdown formatting is supported (i.e. without HTML, images, or links), utilizing dedicated UI components is generally preferred for a richer and more structured presentation.
  - variant: Must be one of: 'caption', 'body', 'title'
  - weight: The relative weight of this component within a Row or Column. This is similar to the CSS 'flex-grow' property. Note: this may ONLY be set when the component is a direct descendant of a Row or Column.
• TextField(label, value, placeholder?, variant? (static), weight? (static), checks? (static))
  - label: The text label for the input field.
  - value: The value of the text field.
  - placeholder: The placeholder text for the input field.
  - variant: The type of input field to display. Must be one of: 'longText', 'number', 'shortText', 'obscured'
  - weight: The relative weight of this component within a Row or Column. This is similar to the CSS 'flex-grow' property. Note: this may ONLY be set when the component is a direct descendant of a Row or Column.

## Positional Function Signatures

Use these exact positional signatures to instantiate check rules or logic functions:
• and(values)
  - Description: Performs a logical AND operation on a list of boolean values.
  - values: The list of boolean values to evaluate.
• calculate(op?, left?, right?)
  - Description: Perform arithmetic on numbers or decimal text. An invalid operand, overflow, or division by zero produces null.
• choose(when?, yes?, no?)
  - Description: Return yes when the condition is true, otherwise no.
• compare(op, left?, right?)
  - Description: Compare scalars with eq/ne or numbers (including decimal text) with lt/le/gt/ge. Null equals null.
• count(values?)
  - Description: Count the items in a list. A missing list counts as zero.
• display(value?, fallback, decimals?)
  - Description: Display a scalar or list. For an invalid calculation, display the supplied fallback. Optional fixed decimal precision applies to numbers.
• email(value)
  - Description: Checks that the value is a valid email address.
• formatNumber(value, decimals?, grouping?)
  - Description: Formats a number with the specified grouping and decimal precision.
  - value: The number to format.
  - decimals: Optional. The number of decimal places to show. Defaults to 0 or 2 depending on locale.
  - grouping: Optional. If true, uses locale-specific grouping separators (e.g. '1,000'). If false, returns raw digits (e.g. '1000'). Defaults to true.
• length(value, min?, max?)
  - Description: Checks string length constraints.
  - min: The minimum allowed length.
  - max: The maximum allowed length.
• not(value)
  - Description: Performs a logical NOT operation on a boolean value.
  - value: The boolean value to negate.
• numberInRange(value?, min, max?)
  - Description: Check numeric text or a number against inclusive bounds. Use for TextField number checks.
• numeric(value, min?, max?)
  - Description: Checks numeric range constraints.
  - min: The minimum allowed value.
  - max: The maximum allowed value.
• or(values)
  - Description: Performs a logical OR operation on a list of boolean values.
  - values: The list of boolean values to evaluate.
• present(value?)
  - Description: Check for nonblank text, a finite number, a boolean, or a nonempty list of these values.
• required(value)
  - Description: Checks that the value is not null, undefined, or empty.
  - value: The value to check.
• round(value?, mode?)
  - Description: Round to an integer, floor or ceil. Returns null for invalid input.
• sameSet(left?, right?)
  - Description: Compare two lists of scalar values, ignoring order and duplicates.
• sum(values?)
  - Description: Add the numbers, decimal text, or booleans (true=1) in a list. Invalid items produce null.
