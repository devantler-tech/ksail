package eksinventory

// Admission-only reads stay separate from the diagnostic's output families.
func capacityReadOperations() []operation {
	return []operation{
		{service: "sts", command: "get-caller-identity"},
		{service: ec2Service, command: "describe-instance-types"},
		{service: ec2Service, command: "describe-capacity-reservations"},
	}
}
