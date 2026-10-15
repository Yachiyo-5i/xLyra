package jsplugin

import (
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
)

// LocalizedText is copy a plugin shows in the admin form. In a manifest it is
// either a plain string, used for every language, or an object keyed by
// language: {"zh": "...", "en": "..."}. The page picks the language it is shown
// in and falls back to English, then to the first one listed.
type LocalizedText map[string]string

const (
	maxTextBytes     = 400
	maxTextLanguages = 8
)

var languageTag = regexp.MustCompile(`^[A-Za-z]{2,3}([-_][A-Za-z0-9]{2,8})?$`)

// UnmarshalJSON accepts a string or an object of strings.
func (t *LocalizedText) UnmarshalJSON(raw []byte) error {
	var plain string
	if err := json.Unmarshal(raw, &plain); err == nil {
		*t = LocalizedText{"": plain}
		return nil
	}
	var byLanguage map[string]string
	if err := json.Unmarshal(raw, &byLanguage); err != nil {
		return fmt.Errorf("expected a string or an object of strings keyed by language")
	}
	*t = LocalizedText(byLanguage)
	return nil
}

// MarshalJSON writes a plain string when there is only one text for all languages.
func (t LocalizedText) MarshalJSON() ([]byte, error) {
	if plain, only := t[""]; only && len(t) == 1 {
		return json.Marshal(plain)
	}
	return json.Marshal(map[string]string(t))
}

// IsZero lets omitempty drop an unset text.
func (t LocalizedText) IsZero() bool { return len(t) == 0 }

// Default is the text for no particular language, for messages in logs and errors.
func (t LocalizedText) Default() string {
	if plain, ok := t[""]; ok {
		return plain
	}
	if en, ok := t["en"]; ok {
		return en
	}
	keys := make([]string, 0, len(t))
	for key := range t {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	if len(keys) == 0 {
		return ""
	}
	return t[keys[0]]
}

func (t LocalizedText) validate() error {
	if len(t) > maxTextLanguages {
		return fmt.Errorf("more than %d languages", maxTextLanguages)
	}
	for language, text := range t {
		if language != "" && !languageTag.MatchString(language) {
			return fmt.Errorf("%q is not a language tag such as zh or en", language)
		}
		if len(text) > maxTextBytes {
			return fmt.Errorf("text is longer than %d bytes", maxTextBytes)
		}
	}
	return nil
}
