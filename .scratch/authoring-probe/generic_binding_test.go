package authoringprobe

import "testing"

type Styled[T any] struct {
	owner T
	anchor *Styled[T]
}

func (s *Styled[T]) StyleBase() *Styled[T] { return s }

func BindStyled[V any, T interface{ *V; StyleBase() *Styled[T] }](owner T) {
if owner == nil { panic("nil owner") }
	s := owner.StyleBase()
	s.owner, s.anchor = owner, s
}

func (s *Styled[T]) Flex() T {
	if s.anchor != s { panic("unbound or copied") }
	return s.owner
}

type Button struct { Styled[*Button]; disabled bool }
func (b *Button) Disabled(v bool) *Button { b.disabled=v; return b }

func TestConcreteChain(t *testing.T) {
	b := &Button{}
	BindStyled(b)
	var result *Button = b.Flex().Disabled(true).Flex()
	if result != b || !b.disabled { t.Fatal("lost owner") }
}

func TestCopiedBuilder(t *testing.T) {
	b := &Button{}
	BindStyled(b)
	copy := *b
	defer func(){ if recover()==nil { t.Error("copied helper accepted") } }()
	copy.Flex()
}
