package router_test

import (
	"context"
	"fmt"

	"github.com/effective-security/gogentic/pkg/llms"
	"github.com/effective-security/gogentic/router"
)

type exampleModel struct{}

func (exampleModel) ValidateInference(llms.InferenceRequest) error { return nil }
func (exampleModel) Infer(context.Context, llms.InferenceRequest) (*llms.InferenceResponse, error) {
	return &llms.InferenceResponse{
		Content: []llms.InferenceBlock{{
			Type: llms.BlockText,
			Text: "Hello",
		}},
		FinishReason: llms.FinishStop,
	}, nil
}

func ExampleNew() {
	r, err := router.New(router.Config{
		Targets: []router.Target{{
			ID:           "chat",
			BackendModel: "configured-model",
			Connector:    exampleModel{},
		}},
	})
	if err != nil {
		panic(err)
	}
	result, err := r.Generate(context.Background(), router.Request{
		Model: "chat",
		Input: llms.InferenceRequest{
			Messages: []llms.InferenceMessage{{
				Role: llms.InferenceRoleUser,
				Content: []llms.InferenceBlock{{
					Type: llms.BlockText,
					Text: "Hello",
				}},
			}},
		},
	})
	if err != nil {
		panic(err)
	}
	fmt.Println(result.Model, result.Response.Content[0].Text)
	// Output: chat Hello
}

func ExampleSelectorFunc() {
	selector := router.SelectorFunc(func(_ context.Context, in router.SelectionInput) (router.Decision, error) {
		// Route by operator tags; a real selector classifies the request first.
		for _, c := range in.Candidates {
			if c.Tags["tier"] == "fast" {
				return router.Decision{
					TargetID:       c.ID,
					Classification: "simple",
				}, nil
			}
		}
		return router.Decision{}, fmt.Errorf("no fast tier candidate")
	})
	r, err := router.New(router.Config{
		Selector:  selector,
		Admission: router.ConcurrencyLimit(8),
		Targets: []router.Target{{
			ID:           "chat",
			BackendModel: "configured-model",
			Connector:    exampleModel{},
			Tags: map[string]string{
				"tier": "fast",
			},
		}},
	})
	if err != nil {
		panic(err)
	}
	result, err := r.Generate(context.Background(), router.Request{
		Model: "auto",
		Input: llms.InferenceRequest{
			Messages: []llms.InferenceMessage{{
				Role: llms.InferenceRoleUser,
				Content: []llms.InferenceBlock{{
					Type: llms.BlockText,
					Text: "Hello",
				}},
			}},
		},
	})
	if err != nil {
		panic(err)
	}
	fmt.Println(result.Model, result.Classification)
	// Output: chat simple
}
