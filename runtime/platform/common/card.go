package common

import "github.com/lengzhao/agentkit/cap/delivery"

// Aliases retain the existing renderer and card-builder API.
type (
	Card             = delivery.Card
	CardHeader       = delivery.CardHeader
	CardElement      = delivery.CardElement
	CardMarkdown     = delivery.CardMarkdown
	CardDivider      = delivery.CardDivider
	CardActions      = delivery.CardActions
	CardNote         = delivery.CardNote
	CardListItem     = delivery.CardListItem
	CardSelect       = delivery.CardSelect
	CardSelectOption = delivery.CardSelectOption
	CardButton       = delivery.CardButton
	CardActionLayout = delivery.CardActionLayout
	CardBuilder      = delivery.CardBuilder
	ButtonOption     = delivery.ButtonOption
)

const (
	CardActionLayoutRow          = delivery.CardActionLayoutRow
	CardActionLayoutEqualColumns = delivery.CardActionLayoutEqualColumns
)

func NewCard() *CardBuilder { return delivery.NewCard() }
