package appserver

import "context"

// Conversation is one thread with the agent, opened once and continued across
// turns. It exists so a caller states what it wants - open, take a turn, steer,
// interrupt - without carrying the thread identifier through every call.
type Conversation struct {
	client  *Client
	options ThreadOptions

	threadID string
}

func NewConversation(client *Client, options ThreadOptions) *Conversation {
	return &Conversation{client: client, options: options}
}

func (c *Conversation) Open(ctx context.Context) (string, error) {
	if c.threadID != "" {
		return c.threadID, nil
	}

	threadID, err := c.client.StartThread(ctx, c.options)
	if err != nil {
		return "", err
	}
	c.threadID = threadID

	return threadID, nil
}

func (c *Conversation) Turn(ctx context.Context, text string) (string, error) {
	threadID, err := c.Open(ctx)
	if err != nil {
		return "", err
	}

	return c.client.StartTurn(ctx, threadID, text)
}

func (c *Conversation) Steer(ctx context.Context, turnID, text string) error {
	return c.client.Steer(ctx, c.threadID, turnID, text)
}

func (c *Conversation) Interrupt(ctx context.Context, turnID string) error {
	return c.client.Interrupt(ctx, c.threadID, turnID)
}

func (c *Conversation) Events() <-chan Event { return c.client.Events() }
