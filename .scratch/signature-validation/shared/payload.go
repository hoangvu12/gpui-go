package shared

type Payload struct{ Value int }
type PayloadAlias = Payload
type OtherPayload struct{ Value int }
type InterfacePayload interface{ Label() string }
type Label string

func (l Label) Label() string { return string(l) }
