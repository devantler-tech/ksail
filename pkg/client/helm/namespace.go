package helm

import (
	"fmt"
	"os"
)

func (c *Client) switchNamespace(namespace string) (func(), error) {
	if namespace == "" {
		return func() {}, nil
	}

	previousNamespace := c.settings.Namespace()
	if previousNamespace == namespace {
		return func() {}, nil
	}

	c.settings.SetNamespace(namespace)

	reinitErr := c.actionConfig.Init(
		c.settings.RESTClientGetter(),
		namespace,
		os.Getenv("HELM_DRIVER"),
	)
	if reinitErr != nil {
		_ = c.restoreNamespace(previousNamespace)

		return nil, fmt.Errorf("failed to set helm namespace %q: %w", namespace, reinitErr)
	}

	return func() {
		restoreErr := c.restoreNamespace(previousNamespace)
		if restoreErr != nil {
			c.debugLog("failed to restore helm namespace: %v", restoreErr)
		}
	}, nil
}

func (c *Client) restoreNamespace(namespace string) error {
	c.settings.SetNamespace(namespace)

	err := c.actionConfig.Init(
		c.settings.RESTClientGetter(),
		namespace,
		os.Getenv("HELM_DRIVER"),
	)
	if err != nil {
		return fmt.Errorf("init action config for namespace %q: %w", namespace, err)
	}

	return nil
}
