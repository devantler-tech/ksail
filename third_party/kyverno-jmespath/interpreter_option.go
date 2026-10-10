package jmespath

type InterpreterOption func(interpreterOptions) interpreterOptions

type interpreterOptions struct {
	functionCaller *FunctionCaller
}

func WithFunctionCaller(functionCaller *FunctionCaller) InterpreterOption {
	return func(o interpreterOptions) interpreterOptions {
		o.functionCaller = functionCaller
		return o
	}
}
