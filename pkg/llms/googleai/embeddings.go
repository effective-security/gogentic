package googleai

/*
// CreateEmbedding creates embeddings from texts.
func (g *GoogleAI) CreateEmbedding(ctx context.Context, texts []string) ([][]float32, error) {
	em := g.embeddingModel

	results := make([][]float32, 0, len(texts))

	batch := em.NewBatch()
	for i, t := range texts {
		batch.AddContent(genai.Text(t))
		// The Gemini Embedding Batch API allows up to 100 documents per batch,
		// so send a request every 100 documents and when we hit the
		// last document.
		if (i > 0 && (i+1)%100 == 0) || i == len(texts)-1 {
			resp, err := em.BatchEmbedContents(ctx, batch)
			if err != nil {
				return nil, errors.Wrap(err, "failed to create embeddings")
			}
			for _, e := range resp.Embeddings {
				results = append(results, e.Values)
			}
			batch = em.NewBatch()
		}
	}

	return results, nil
}
*/
