package chatmodel

import "strings"

// String is a simple string type that implements the ContentProvider interface.
type String struct {
	value string
}

func NewString(str string) *String {
	return &String{
		value: str,
	}
}

// GetContent gets the content of the message for the chat history
func (o String) GetContent() string {
	return string(o.value)
}

func (s String) String() string {
	return string(s.value)
}

func (s String) Bytes() []byte {
	return []byte(s.value)
}

func (s *String) Unmarshal(bs []byte) error {
	str := strings.Trim(string(bs), "\"")

	*s = String{value: str}
	return nil
}
