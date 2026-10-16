package eksinventory

func operations() []operation {
	result := clusterOperations()
	result = append(result, fleetOperations()...)
	result = append(result, computeOperations()...)
	result = append(result, networkOperations()...)
	result = append(result, storageOperations()...)
	result = append(result, quotaOperations()...)

	return result
}

func clusterOperations() []operation {
	return []operation{
		{
			service:     eksService,
			command:     "list-clusters",
			title:       "EKS clusters",
			collection:  "clusters",
			kind:        "names",
			inputToken:  eksTokenKey,
			outputToken: eksTokenKey,
			pageSize:    "maxResults",
		},
		{
			service:    cloudFormationService,
			command:    "list-stacks",
			title:      "CloudFormation stacks, by state",
			collection: "StackSummaries",
			identity:   "StackId",
			state:      "StackStatus",
			allowed: "CREATE_IN_PROGRESS CREATE_FAILED CREATE_COMPLETE ROLLBACK_IN_PROGRESS ROLLBACK_FAILED " +
				"ROLLBACK_COMPLETE DELETE_IN_PROGRESS DELETE_FAILED DELETE_COMPLETE UPDATE_IN_PROGRESS " +
				"UPDATE_COMPLETE_CLEANUP_IN_PROGRESS UPDATE_COMPLETE UPDATE_FAILED UPDATE_ROLLBACK_IN_PROGRESS " +
				"UPDATE_ROLLBACK_FAILED UPDATE_ROLLBACK_COMPLETE_CLEANUP_IN_PROGRESS UPDATE_ROLLBACK_COMPLETE " +
				"REVIEW_IN_PROGRESS IMPORT_IN_PROGRESS IMPORT_COMPLETE IMPORT_ROLLBACK_IN_PROGRESS " +
				"IMPORT_ROLLBACK_FAILED IMPORT_ROLLBACK_COMPLETE ",
			inputToken:  nextTokenKey,
			outputToken: nextTokenKey,
		},
	}
}

func fleetOperations() []operation {
	return []operation{
		{
			service:     ec2Service,
			command:     describeFleets,
			title:       "Enumerated EC2 fleets, by state and type",
			collection:  "Fleets",
			identity:    "FleetId",
			state:       "FleetState",
			allowed:     "submitted active deleted failed deleted_running deleted_terminating modifying",
			extra:       "Type",
			kind:        fleetKind,
			inputToken:  nextTokenKey,
			outputToken: nextTokenKey,
			pageSize:    maxResultsKey,
		},
		{
			service:     ec2Service,
			command:     "describe-spot-fleet-requests",
			title:       "Spot fleet requests, by state",
			collection:  "SpotFleetRequestConfigs",
			identity:    "SpotFleetRequestId",
			state:       "SpotFleetRequestState",
			allowed:     "submitted active cancelled cancelled_running cancelled_terminating failed modifying",
			inputToken:  nextTokenKey,
			outputToken: nextTokenKey,
			pageSize:    maxResultsKey,
		},
		{
			service:     ec2Service,
			command:     "describe-spot-instance-requests",
			title:       "Spot instance requests, by state",
			collection:  "SpotInstanceRequests",
			identity:    "SpotInstanceRequestId",
			state:       stateField,
			allowed:     "open active closed cancelled failed disabled",
			inputToken:  nextTokenKey,
			outputToken: nextTokenKey,
			pageSize:    maxResultsKey,
		},
	}
}

func computeOperations() []operation {
	return []operation{
		{
			service:     ec2Service,
			command:     describeInstances,
			title:       "Instances not terminated, by state and type",
			collection:  reservationsInstances,
			identity:    instanceIDField,
			state:       "State.Name",
			allowed:     "pending running shutting-down terminated stopping stopped",
			extra:       "InstanceType",
			kind:        instanceKind,
			inputToken:  nextTokenKey,
			outputToken: nextTokenKey,
			pageSize:    maxResultsKey,
		},
		{
			service:     autoScalingService,
			command:     describeAutoScalingGroups,
			title:       "Auto Scaling groups, by desired capacity",
			collection:  autoScalingGroupsCollection,
			identity:    "AutoScalingGroupARN",
			state:       "DesiredCapacity",
			kind:        "capacity",
			inputToken:  nextTokenKey,
			outputToken: nextTokenKey,
			pageSize:    "MaxRecords",
		},
	}
}

func networkOperations() []operation {
	return []operation{
		{
			service:     ec2Service,
			command:     describeNatGateways,
			title:       "NAT gateways, by state",
			collection:  natGatewaysCollection,
			identity:    natGatewayIDField,
			state:       stateField,
			allowed:     "pending failed available deleting deleted",
			inputToken:  nextTokenKey,
			outputToken: nextTokenKey,
			pageSize:    maxResultsKey,
		},
		{
			service:    ec2Service,
			command:    describeAddresses,
			title:      "Elastic IP addresses",
			collection: "Addresses",
			identity:   "AllocationId",
		},
		{
			service:     ec2Service,
			command:     describeNetworkInterfaces,
			title:       "Network interfaces, by state",
			collection:  networkInterfacesCollection,
			identity:    networkInterfaceIDField,
			state:       "Status",
			allowed:     "available associated attaching in-use detaching",
			inputToken:  nextTokenKey,
			outputToken: nextTokenKey,
			pageSize:    maxResultsKey,
		},
	}
}

func storageOperations() []operation {
	return []operation{
		{
			service:     ec2Service,
			command:     describeVolumes,
			title:       "EBS volumes, by state",
			collection:  "Volumes",
			identity:    "VolumeId",
			state:       stateField,
			allowed:     "creating available in-use deleting deleted error",
			inputToken:  nextTokenKey,
			outputToken: nextTokenKey,
			pageSize:    maxResultsKey,
		},
		{
			service:     elbService,
			command:     describeLoadBalancers,
			title:       "Classic load balancers",
			collection:  classicLoadBalancersCollection,
			identity:    classicLoadBalancerNameField,
			inputToken:  markerKey,
			outputToken: nextMarkerKey,
			pageSize:    pageSizeKey,
		},
		{
			service:     elbv2Service,
			command:     describeLoadBalancers,
			title:       "ELBv2 load balancers, by state",
			collection:  loadBalancersCollection,
			identity:    loadBalancerARNField,
			state:       "State.Code",
			allowed:     "active provisioning active_impaired failed",
			inputToken:  markerKey,
			outputToken: nextMarkerKey,
			pageSize:    pageSizeKey,
		},
	}
}

func quotaOperations() []operation {
	return []operation{
		{
			service:     "service-quotas",
			command:     "list-service-quotas",
			title:       "Applied launch quotas, by name and value",
			collection:  "Quotas",
			identity:    "QuotaCode",
			kind:        quotaKind,
			inputToken:  nextTokenKey,
			outputToken: nextTokenKey,
			pageSize:    maxResultsKey,
		},
	}
}
