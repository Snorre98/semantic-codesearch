from __future__ import annotations

import ollama

from mcp_code_search.config import Config


class EmbeddingClient:
    def __init__(self, config: Config) -> None:
        self.client = ollama.Client(host=config.ollama_base_url)
        self.model = config.embedding_model

    def embed_batch(self, texts: list[str]) -> list[list[float]]:
        """Embed multiple texts in a single request."""
        if not texts:
            return []
        response = self.client.embed(model=self.model, input=texts)
        return response["embeddings"]

    def embed_single(self, text: str) -> list[float]:
        """Embed a single text string."""
        response = self.client.embed(model=self.model, input=text)
        return response["embeddings"][0]
