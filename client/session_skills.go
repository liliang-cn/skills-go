package client

import (
	"strings"

	"github.com/openai/openai-go/v3"
)

type skillSession struct {
	order  []string
	active map[string]*ActivatedSkill
}

func (c *Client) rememberSessionSkill(sessionID string, activated *ActivatedSkill) {
	if sessionID == "" || activated == nil || !c.replaySessionSkills {
		return
	}

	c.sessionsMu.Lock()
	defer c.sessionsMu.Unlock()

	session := c.sessions[sessionID]
	if session == nil {
		session = &skillSession{active: make(map[string]*ActivatedSkill)}
		c.sessions[sessionID] = session
	}

	if _, exists := session.active[activated.Name]; !exists {
		session.order = append(session.order, activated.Name)
	}

	copyActivated := *activated
	copyActivated.Resources = append([]string(nil), activated.Resources...)
	session.active[activated.Name] = &copyActivated
}

func (c *Client) sessionActiveSet(sessionID string) map[string]bool {
	result := make(map[string]bool)
	if sessionID == "" || !c.replaySessionSkills {
		return result
	}

	c.sessionsMu.RLock()
	defer c.sessionsMu.RUnlock()

	session := c.sessions[sessionID]
	if session == nil {
		return result
	}
	for name := range session.active {
		result[name] = true
	}
	return result
}

func (c *Client) sessionProtectedMessages(sessionID string, history []openai.ChatCompletionMessageParamUnion) []openai.ChatCompletionMessageParamUnion {
	if sessionID == "" || !c.replaySessionSkills {
		return nil
	}

	c.sessionsMu.RLock()
	session := c.sessions[sessionID]
	if session == nil || len(session.order) == 0 {
		c.sessionsMu.RUnlock()
		return nil
	}

	var wrapped []string
	for _, name := range session.order {
		activated := session.active[name]
		if activated == nil || historyContainsWrappedSkill(history, activated.Wrapped) {
			continue
		}
		wrapped = append(wrapped, activated.Wrapped)
	}
	c.sessionsMu.RUnlock()

	if len(wrapped) == 0 {
		return nil
	}

	content := "The following activated skill content remains in effect for this session. Preserve and follow it unless superseded.\n\n" + strings.Join(wrapped, "\n\n")
	return []openai.ChatCompletionMessageParamUnion{openai.SystemMessage(content)}
}

// ActiveSessionSkills returns the activated skills remembered for a session.
func (c *Client) ActiveSessionSkills(sessionID string) []ActivatedSkill {
	if sessionID == "" {
		return nil
	}

	c.sessionsMu.RLock()
	defer c.sessionsMu.RUnlock()

	session := c.sessions[sessionID]
	if session == nil {
		return nil
	}

	result := make([]ActivatedSkill, 0, len(session.order))
	for _, name := range session.order {
		activated := session.active[name]
		if activated == nil {
			continue
		}
		copyActivated := *activated
		copyActivated.Resources = append([]string(nil), activated.Resources...)
		result = append(result, copyActivated)
	}
	return result
}

// ClearSessionSkills clears remembered active skills for a session.
func (c *Client) ClearSessionSkills(sessionID string) {
	if sessionID == "" {
		return
	}

	c.sessionsMu.Lock()
	defer c.sessionsMu.Unlock()

	delete(c.sessions, sessionID)
}

func historyContainsWrappedSkill(history []openai.ChatCompletionMessageParamUnion, wrapped string) bool {
	if wrapped == "" {
		return false
	}
	for _, message := range history {
		for _, text := range messageTexts(message) {
			if strings.Contains(text, wrapped) {
				return true
			}
		}
	}
	return false
}

func messageTexts(message openai.ChatCompletionMessageParamUnion) []string {
	var texts []string

	if message.OfSystem != nil && message.OfSystem.Content.OfString.Valid() {
		texts = append(texts, message.OfSystem.Content.OfString.Value)
	}
	if message.OfDeveloper != nil && message.OfDeveloper.Content.OfString.Valid() {
		texts = append(texts, message.OfDeveloper.Content.OfString.Value)
	}
	if message.OfUser != nil && message.OfUser.Content.OfString.Valid() {
		texts = append(texts, message.OfUser.Content.OfString.Value)
	}
	if message.OfAssistant != nil && message.OfAssistant.Content.OfString.Valid() {
		texts = append(texts, message.OfAssistant.Content.OfString.Value)
	}
	if message.OfTool != nil && message.OfTool.Content.OfString.Valid() {
		texts = append(texts, message.OfTool.Content.OfString.Value)
	}
	if message.OfFunction != nil && message.OfFunction.Content.Valid() {
		texts = append(texts, message.OfFunction.Content.Value)
	}

	return texts
}
