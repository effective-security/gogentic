package dummy

import (
	"encoding/json"
)

type Stringer interface {
	String() string
}

type Unmarshaler interface {
	Unmarshal(bs []byte) error
}

type Encoder struct{}

func NewEncoder() *Encoder {
	return new(Encoder)
}

func (e *Encoder) Marshal(v any) ([]byte, error) {
	if s, ok := v.(Stringer); ok {
		return []byte(s.String()), nil
	} else if s, ok := v.(string); ok {
		return []byte(s), nil
	} else if s, ok := v.([]byte); ok {
		return s, nil
	} else if s, ok := v.(*string); ok {
		return []byte(*s), nil
	} else if s, ok := v.(*[]byte); ok {
		return *s, nil
	}
	return json.Marshal(v)
}

func (e *Encoder) Unmarshal(bs []byte, ret any) error {
	if s, ok := ret.(Unmarshaler); ok {
		return s.Unmarshal(bs)
	} else if s, ok := ret.(*string); ok {
		*s = string(bs)
	} else if s, ok := ret.(*[]byte); ok {
		*s = bs
	} else if err := json.Unmarshal(bs, ret); err != nil {
		return err
	}
	return nil
}

func (e *Encoder) Validate(req any) error {
	return nil
}

func (e *Encoder) GetFormatInstructions() string {
	return ""
}
