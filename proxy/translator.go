package main

import ()

// TranslateRequest is a no-op since Z.AI natively supports the Claude Code API.
// Previous translations (stripping thinking, flattening system arrays, removing
// cache_control) were causing 422 errors because Z.AI expects these fields.
func TranslateRequest(body []byte) ([]byte, bool, error) {
	return body, false, nil
}
