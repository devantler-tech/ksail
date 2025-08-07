package cmd

import (
	"testing"
)

func TestRootCommand(t *testing.T) {
	// Test that the root command can be created without panicking
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("Root command creation panicked: %v", r)
		}
	}()

	// This will initialize all commands
	_ = rootCmd
}

func TestCommandsExist(t *testing.T) {
	expectedCommands := []string{
		"init",
		"up", 
		"down",
		"status",
		"list",
		"start",
		"stop", 
		"update",
		"connect",
		"validate",
		"gen",
		"secrets",
	}

	for _, cmdName := range expectedCommands {
		cmd, _, err := rootCmd.Find([]string{cmdName})
		if err != nil {
			t.Errorf("Command %s not found: %v", cmdName, err)
		}
		if cmd == nil {
			t.Errorf("Command %s is nil", cmdName)
		}
		if cmd.Name() != cmdName {
			t.Errorf("Expected command name %s, got %s", cmdName, cmd.Name())
		}
	}
}