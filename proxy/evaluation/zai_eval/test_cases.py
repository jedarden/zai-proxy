"""Test case definitions for evaluation framework."""

from zai_eval.models import EvaluationRequest


# Diverse test cases covering different request types
TEST_CASES = [
    EvaluationRequest(
        name="short_simple",
        description="Short simple text",
        model="claude-3-sonnet-20240229",
        max_tokens=50,
        messages=[{"role": "user", "content": "Hello, how are you?"}],
    ),
    EvaluationRequest(
        name="medium_conversation",
        description="Medium length conversation",
        model="claude-3-sonnet-20240229",
        max_tokens=100,
        messages=[
            {"role": "user", "content": "What is the capital of France?"},
            {"role": "assistant", "content": "The capital of France is Paris."},
            {"role": "user", "content": "Tell me more about it."},
        ],
    ),
    EvaluationRequest(
        name="long_context",
        description="Long context with detailed information",
        model="claude-3-sonnet-20240229",
        max_tokens=150,
        messages=[
            {
                "role": "user",
                "content": """The Industrial Revolution was a period of major industrialization and innovation that took place during the late 1700s and early 1800s. The Industrial Revolution began in Great Britain and quickly spread throughout the world. The use of new basic materials, primarily iron and steel, was a key factor. The use of new energy sources, including both fuels and motive power, such as coal, the steam engine, electricity, petroleum, and the internal-combustion engine, was also important. The invention of new machines, including the spinning jenny and the power loom, allowed for increased production with fewer workers. The factory system was a new way of organizing labor, where many workers were brought together in one place to produce goods under the supervision of a manager. This system led to increased efficiency and productivity, but also to poor working conditions and child labor. The development of new transportation methods, such as canals, roads, and railways, allowed for the faster and cheaper movement of goods and people. The Industrial Revolution had a profound impact on society, economy, and culture, and laid the groundwork for many of the technological advancements we enjoy today.""",
            }
        ],
    ),
    EvaluationRequest(
        name="code_snippet",
        description="Request involving code",
        model="claude-3-sonnet-20240229",
        max_tokens=100,
        messages=[
            {
                "role": "user",
                "content": """Write a function in Python to calculate the factorial of a number:

```python
def factorial(n):
    # Your code here
```""",
            }
        ],
    ),
    EvaluationRequest(
        name="multi_turn_conversation",
        description="Multiple turns of conversation",
        model="claude-3-sonnet-20240229",
        max_tokens=100,
        messages=[
            {"role": "user", "content": "I want to learn Python."},
            {"role": "assistant", "content": "That's great! Python is a versatile programming language. Where would you like to start?"},
            {"role": "user", "content": "Let's start with variables and data types."},
            {"role": "assistant", "content": "Python has several built-in data types including integers, floats, strings, booleans, lists, tuples, dictionaries, and sets. Variables are created by assignment, no need to declare types."},
            {"role": "user", "content": "Can you show me an example?"},
        ],
    ),
    EvaluationRequest(
        name="structured_data",
        description="Request with structured data format",
        model="claude-3-sonnet-20240229",
        max_tokens=100,
        messages=[
            {
                "role": "user",
                "content": """Here is some data in JSON format:
```json
{
  "name": "Alice",
  "age": 30,
  "city": "New York",
  "hobbies": ["reading", "hiking", "photography"]
}
```
Extract the hobbies and create a summary.""",
            }
        ],
    ),
    EvaluationRequest(
        name="mathematical_content",
        description="Content with mathematical expressions",
        model="claude-3-sonnet-20240229",
        max_tokens=100,
        messages=[
            {
                "role": "user",
                "content": """Solve this equation step by step: 2x + 5 = 13. Show your work and explain each step.""",
            }
        ],
    ),
    EvaluationRequest(
        name="multilingual_text",
        description="Text with multiple languages",
        model="claude-3-sonnet-20240229",
        max_tokens=100,
        messages=[
            {
                "role": "user",
                "content": """Translate and explain the meaning of these phrases:
1. Spanish: "Hola, ¿cómo estás?"
2. French: "Bonjour, comment allez-vous?"
3. German: "Guten Tag, wie geht es Ihnen?"
4. Japanese: "こんにちは、元気ですか？"
5. Chinese: "你好，你好吗？""",
            }
        ],
    ),
    EvaluationRequest(
        name="list_heavy_content",
        description="Content with many list items",
        model="claude-3-sonnet-20240229",
        max_tokens=100,
        messages=[
            {
                "role": "user",
                "content": """Here are 10 programming best practices:
1. Write clear and descriptive names
2. Keep functions small and focused
3. Don't repeat yourself (DRY)
4. Comment your code
5. Use version control
6. Test your code
7. Handle errors gracefully
8. Optimize for readability
9. Follow style guides
10. Keep learning

Explain why these are important.""",
            }
        ],
    ),
    EvaluationRequest(
        name="json_only_response",
        description="Request expecting JSON response",
        model="claude-3-sonnet-20240229",
        max_tokens=150,
        messages=[
            {
                "role": "user",
                "content": """Create a JSON object representing a book with these fields: title, author, publication_year, genres (array), and rating (1-5). Respond with only the JSON, no explanation.""",
            }
        ],
    ),
    EvaluationRequest(
        name="creative_writing",
        description="Creative writing prompt",
        model="claude-3-sonnet-20240229",
        max_tokens=100,
        messages=[
            {
                "role": "user",
                "content": """Write a short opening paragraph for a mystery novel set in a small coastal town. Include atmospheric details and a hint of something unusual.""",
            }
        ],
    ),
    EvaluationRequest(
        name="technical_explanation",
        description="Technical concept explanation",
        model="claude-3-sonnet-20240229",
        max_tokens=150,
        messages=[
            {
                "role": "user",
                "content": """Explain the concept of microservices architecture, its advantages over monolithic architecture, and the challenges involved in implementing it. Include specific examples.""",
            }
        ],
    ),
    EvaluationRequest(
        name="empty_system_message",
        description="Request with system message",
        model="claude-3-sonnet-20240229",
        max_tokens=100,
        messages=[
            {"role": "system", "content": "You are a helpful assistant."},
            {"role": "user", "content": "What is 2+2?"},
        ],
    ),
    EvaluationRequest(
        name="special_characters",
        description="Text with many special characters and symbols",
        model="claude-3-sonnet-20240229",
        max_tokens=100,
        messages=[
            {
                "role": "user",
                "content": """Explain what these special characters mean in programming: @, #, $, %, ^, &, *, _, +, =, {, }, [, ], |, \\, :, ;, ", ', <, >, ?, /, ~""",
            }
        ],
    ),
]


def get_test_cases() -> list[EvaluationRequest]:
    """Return all test cases."""
    return TEST_CASES


def get_test_case_by_name(name: str) -> EvaluationRequest | None:
    """Get a specific test case by name."""
    for case in TEST_CASES:
        if case.name == name:
            return case
    return None
