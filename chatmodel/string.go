package chatmodel

import "strings"

// String is a simple string type that implements the ContentProvider interface.
type String struct {
	value string
}

// NewString constructs a String with the provided initial value.
func NewString(str string) *String {
	return &String{
		value: str,
	}
}

// ParseInput sets the string value from raw input. Satisfies InputParser.
func (o *String) ParseInput(input string) error {
	o.value = input
	return nil
}

// GetContent returns the underlying string content. Satisfies ContentProvider.
func (o String) GetContent() string {
	return string(o.value)
}

// String returns the underlying string.
func (s String) String() string {
	return string(s.value)
}

// Bytes returns the underlying value as a byte slice.
func (s String) Bytes() []byte {
	return []byte(s.value)
}

// Unmarshal populates s from a JSON string or raw bytes, trimming quotes if
// present.
func (s *String) Unmarshal(bs []byte) error {
	str := strings.Trim(string(bs), "\"")

	*s = String{value: str}
	return nil
}
