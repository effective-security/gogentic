package dummy

import (
	"context"
)

type StreamEncoder struct{}

func NewStreamEncoder() *StreamEncoder {
	return new(StreamEncoder)
}

func (e *StreamEncoder) EnableValidate() {}

func (e *StreamEncoder) GetFormatInstructions() string {
	return ""
}

func (e *StreamEncoder) Read(ctx context.Context, ch <-chan string) <-chan any {
	parsedChan := make(chan any)
	go func() {
		defer close(parsedChan)
		for {
			select {
			case <-ctx.Done():
				return
			case text, ok := <-ch:
				if !ok {
					// Stream closed
					return
				}
				parsedChan <- text
			}
		}
	}()
	return parsedChan
}
