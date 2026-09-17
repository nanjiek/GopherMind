package service

import "gophermind/internal/core/model"

const NoDataFallbackAnswer = "我在当前知识库中未检索到该问题的可靠信息。请换个关键词，或补充更具体的疾病/药物名称。"

func fallbackUsage() model.Usage {
	return model.Usage{
		Provider:     "retrieval-guardrail",
		InputTokens:  0,
		OutputTokens: 0,
	}
}
