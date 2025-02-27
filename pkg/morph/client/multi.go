package client

import (
	"sort"

	"go.uber.org/zap"
)

// Endpoint represents morph endpoint together with its priority.
type Endpoint struct {
	Address  string
	Priority int
}

type endpoints struct {
	curr int
	list []Endpoint
}

func (e *endpoints) init(ee []Endpoint) {
	sort.SliceStable(ee, func(i, j int) bool {
		return ee[i].Priority < ee[j].Priority
	})

	e.curr = 0
	e.list = ee
}

func (c *Client) switchRPC() bool {
	c.switchLock.Lock()
	defer c.switchLock.Unlock()

	c.client.Close()

	// Iterate endpoints in the order of decreasing priority.
	// Skip the current endpoint.
	for c.endpoints.curr = range c.endpoints.list {
		newEndpoint := c.endpoints.list[c.endpoints.curr].Address
		cli, err := newWSClient(c.cfg, newEndpoint)
		if err != nil {
			c.logger.Warn("could not establish connection to the switched RPC node",
				zap.String("endpoint", newEndpoint),
				zap.Error(err),
			)

			continue
		}

		err = cli.Init()
		if err != nil {
			cli.Close()
			c.logger.Warn("could not init the switched RPC node",
				zap.String("endpoint", newEndpoint),
				zap.Error(err),
			)

			continue
		}

		c.cache.invalidate()
		c.client = cli

		c.logger.Info("connection to the new RPC node has been established",
			zap.String("endpoint", newEndpoint))

		if !c.restoreSubscriptions(newEndpoint) {
			// new WS client does not allow
			// restoring subscription, client
			// could not work correctly =>
			// closing connection to RPC node
			// to switch to another one
			cli.Close()
			continue
		}

		return true
	}

	return false
}

func (c *Client) notificationLoop() {
	for {
		select {
		case <-c.cfg.ctx.Done():
			_ = c.UnsubscribeAll()
			c.close()

			return
		case <-c.closeChan:
			_ = c.UnsubscribeAll()
			c.close()

			return
		case n, ok := <-c.client.Notifications:
			// notification channel is used as a connection
			// state: if it is closed, the connection is
			// considered to be lost
			if !ok {
				var closeReason string
				if closeErr := c.client.GetError(); closeErr != nil {
					closeReason = closeErr.Error()
				} else {
					closeReason = "unknown"
				}

				c.logger.Warn("switching to the next RPC node",
					zap.String("reason", closeReason),
				)

				if !c.switchRPC() {
					c.logger.Error("could not establish connection to any RPC node")

					// could not connect to all endpoints =>
					// switch client to inactive mode
					c.inactiveMode()

					return
				}

				// TODO(@carpawell): call here some callback retrieved in constructor
				// of the client to allow checking chain state since during switch
				// process some notification could be lost

				continue
			}

			c.notifications <- n
		}
	}
}

// close closes notification channel and wrapped WS client
func (c *Client) close() {
	close(c.notifications)
	c.client.Close()
}
