package sample

type IDAccessor interface {
	GetID() int
	SetID(int)
}

type Bar struct {
	id int
}

func (b *Bar) GetID() int {
	return b.id
}

func (b *Bar) SetID(i int) {
	b.id = i
}

type Baz struct {
	Bar
}

func (b *Baz) GetID() int {
	return add(b.id, 10000)
}

func (b *Baz) SetID(i int) {
	b.id = i - 10000
}

func add(a, b int) int {
	return a + b
}
