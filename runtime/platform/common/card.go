package common

// Card is a platform-agnostic rich message (Slack Block Kit, Feishu interactive card, etc.).
type Card struct {
	Header   *CardHeader
	Elements []CardElement
}

type CardHeader struct {
	Title string
	Color string // blue, green, orange, red, ...
}

type CardElement interface {
	cardElement()
}

type CardMarkdown struct{ Content string }
type CardDivider struct{}
type CardActions struct {
	Buttons []CardButton
	Layout  CardActionLayout
}
type CardNote struct {
	Text string
	Tag  string
}
type CardListItem struct {
	Text     string
	BtnText  string
	BtnType  string
	BtnValue string
	Extra    map[string]string
}

func (CardMarkdown) cardElement() {}
func (CardDivider) cardElement()  {}
func (CardActions) cardElement()  {}
func (CardNote) cardElement()     {}
type CardSelect struct {
	Placeholder string
	Options     []CardSelectOption
	InitValue   string
}

type CardSelectOption struct {
	Text  string
	Value string
}

func (CardSelect) cardElement() {}
func (CardListItem) cardElement() {}

type CardButton struct {
	Text  string
	Type  string
	Value string
	Extra map[string]string
}

type CardActionLayout string

const (
	CardActionLayoutRow          CardActionLayout = "row"
	CardActionLayoutEqualColumns CardActionLayout = "equal_columns"
)

type CardBuilder struct{ card Card }

func NewCard() *CardBuilder { return &CardBuilder{} }

func (b *CardBuilder) Title(title, color string) *CardBuilder {
	b.card.Header = &CardHeader{Title: title, Color: color}
	return b
}

func (b *CardBuilder) Markdown(content string) *CardBuilder {
	if content != "" {
		b.card.Elements = append(b.card.Elements, CardMarkdown{Content: content})
	}
	return b
}

func (b *CardBuilder) Note(text string) *CardBuilder {
	if text != "" {
		b.card.Elements = append(b.card.Elements, CardNote{Text: text})
	}
	return b
}

func (b *CardBuilder) ListItemBtnExtra(desc, btnText, btnType, btnValue string, extra map[string]string) *CardBuilder {
	b.card.Elements = append(b.card.Elements, CardListItem{
		Text: desc, BtnText: btnText, BtnType: btnType, BtnValue: btnValue, Extra: extra,
	})
	return b
}

func (b *CardBuilder) Build() *Card {
	c := b.card
	return &c
}

// CollectButtons flattens list-item buttons for Telegram inline keyboards.
func (c *Card) CollectButtons() [][]ButtonOption {
	var rows [][]ButtonOption
	for _, elem := range c.Elements {
		e, ok := elem.(CardListItem)
		if !ok {
			continue
		}
		rows = append(rows, []ButtonOption{{Text: e.BtnText, Data: e.BtnValue}})
	}
	return rows
}

type ButtonOption struct {
	Text string
	Data string
}

func (c *Card) FallbackText() string {
	if c.Header != nil && c.Header.Title != "" {
		return c.Header.Title
	}
	return " "
}
