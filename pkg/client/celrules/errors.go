package celrules

import "errors"

var (
	// ErrRuleCompilation indicates a rule expression failed to compile (a
	// broken rules file); it names the offending rule.
	ErrRuleCompilation = errors.New("cel rule compilation failed")
	// ErrRulesParse indicates the rules file could not be parsed.
	ErrRulesParse = errors.New("cel rules parse failed")
	// ErrUnknownSeverity indicates a rule declared an unrecognised severity.
	ErrUnknownSeverity = errors.New("unknown rule severity")
	// ErrInvalidRule indicates a rule is structurally invalid (e.g. missing a
	// name or expression).
	ErrInvalidRule = errors.New("invalid cel rule")
)
