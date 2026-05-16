package evaluation

// GetTestCases returns a diverse set of test cases for evaluation
func GetTestCases() []TestRequest {
	return []TestRequest{
		{
			Name: "Simple greeting",
			Request: ClaudeRequest{
				Model:     "claude-3-sonnet-20240229",
				MaxTokens: 100,
				Messages: []Message{
					{Role: "user", Content: "Hello! How are you?"},
				},
			},
			Stream: false,
		},
		{
			Name: "Code generation request",
			Request: ClaudeRequest{
				Model:     "claude-3-sonnet-20240229",
				MaxTokens: 500,
				Messages: []Message{
					{Role: "user", Content: "Write a Python function to calculate fibonacci numbers"},
				},
			},
			Stream: false,
		},
		{
			Name: "Multi-turn conversation",
			Request: ClaudeRequest{
				Model:     "claude-3-sonnet-20240229",
				MaxTokens: 300,
				Messages: []Message{
					{Role: "user", Content: "What is the capital of France?"},
					{Role: "assistant", Content: "The capital of France is Paris."},
					{Role: "user", Content: "What is its population?"},
				},
			},
			Stream: false,
		},
		{
			Name: "Long context input",
			Request: ClaudeRequest{
				Model:     "claude-3-sonnet-20240229",
				MaxTokens: 200,
				Messages: []Message{
					{Role: "user", Content: generateLongText(500)},
				},
			},
			Stream: false,
		},
		{
			Name: "JSON response request",
			Request: ClaudeRequest{
				Model:     "claude-3-sonnet-20240229",
				MaxTokens: 300,
				Messages: []Message{
					{Role: "user", Content: "List 5 colors in JSON format with their hex codes"},
				},
			},
			Stream: false,
		},
		{
			Name: "Streaming response",
			Request: ClaudeRequest{
				Model:     "claude-3-sonnet-20240229",
				MaxTokens: 200,
				Messages: []Message{
					{Role: "user", Content: "Tell me a short story about a robot"},
				},
			},
			Stream: true,
		},
		{
			Name: "Technical documentation",
			Request: ClaudeRequest{
				Model:     "claude-3-sonnet-20240229",
				MaxTokens: 400,
				Messages: []Message{
					{Role: "user", Content: "Explain the concept of recursion in computer science with examples"},
				},
			},
			Stream: false,
		},
		{
			Name: "Creative writing",
			Request: ClaudeRequest{
				Model:     "claude-3-sonnet-20240229",
				MaxTokens: 350,
				Messages: []Message{
					{Role: "user", Content: "Write a haiku about cloud computing"},
				},
			},
			Stream: false,
		},
		{
			Name: "Data analysis request",
			Request: ClaudeRequest{
				Model:     "claude-3-sonnet-20240229",
				MaxTokens: 400,
				Messages: []Message{
					{Role: "user", Content: "Analyze the pros and cons of microservices vs monolithic architecture"},
				},
			},
			Stream: false,
		},
		{
			Name: "Short response",
			Request: ClaudeRequest{
				Model:     "claude-3-sonnet-20240229",
				MaxTokens: 50,
				Messages: []Message{
					{Role: "user", Content: "What is 2+2?"},
				},
			},
			Stream: false,
		},
		{
			Name: "Medium response",
			Request: ClaudeRequest{
				Model:     "claude-3-sonnet-20240229",
				MaxTokens: 300,
				Messages: []Message{
					{Role: "user", Content: "Explain the difference between TCP and UDP"},
				},
			},
			Stream: false,
		},
		{
			Name: "List generation",
			Request: ClaudeRequest{
				Model:     "claude-3-sonnet-20240229",
				MaxTokens: 250,
				Messages: []Message{
					{Role: "user", Content: "List 10 common programming paradigms with brief descriptions"},
				},
			},
			Stream: false,
		},
		{
			Name: "Streaming long response",
			Request: ClaudeRequest{
				Model:     "claude-3-sonnet-20240229",
				MaxTokens: 500,
				Messages: []Message{
					{Role: "user", Content: "Write a detailed explanation of how HTTP works, including methods, headers, and status codes"},
				},
			},
			Stream: true,
		},
	}
}

// generateLongText generates repetitive text for testing long inputs
func generateLongText(words int) string {
	baseText := "This is a test sentence with multiple words for token counting purposes. "
	result := ""
	for len(result) < words*5 { // Approximate 5 chars per word
		result += baseText
	}
	maxLen := words * 5
	if maxLen > len(result) {
		maxLen = len(result)
	}
	return result[:maxLen]
}
