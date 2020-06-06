package cluster

import (
	"errors"
	"fmt"
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/awserr"
	"github.com/aws/aws-sdk-go/service/ec2"
	"github.com/aws/aws-sdk-go/service/elbv2"
	"github.com/mumoshu/terraform-provider-eksctl/pkg/awsclicompat"
	"log"
	"sort"
	"strings"
	"time"
)

type ListenerStatus struct {
	Listener       *elbv2.Listener
	Rule           *elbv2.Rule
	ALBAttachments []ALBAttachment

	DesiredTG  *elbv2.TargetGroup
	CurrentTG  *elbv2.TargetGroup
	DeletedTGs *elbv2.TargetGroup

	// Common listener rule settings
	RulePriority int64
	Hosts        []string
	PathPatterns []string
}

// the key is listener ARN
type ListenerStatuses = map[string]ListenerStatus

func planListenerChanges(cluster *Cluster, oldId, newId string) (ListenerStatuses, error) {
	if cluster.VPCID == "" {
		log.Printf("planning listener changes: %+v", cluster)

		return nil, errors.New("planning listener changes: vpc id is required for this operation")
	}

	svc := elbv2.New(awsclicompat.NewSession(cluster.Region))

	//oldClusterName := getClusterName(cluster, oldId)
	//newClusterName := getClusterName(cluster, newId)

	listenerStatuses := map[string]*ListenerStatus{}
	{
		for i := range cluster.ALBAttachments {
			a := cluster.ALBAttachments[i]
			if _, ok := listenerStatuses[a.ListenerARN]; !ok {
				listenerStatuses[a.ListenerARN] = &ListenerStatus{}
			}

			l := listenerStatuses[a.ListenerARN]
			l.ALBAttachments = append(l.ALBAttachments, a)
		}

		var arns []*string
		for arn, _ := range listenerStatuses {
			arns = append(arns, aws.String(arn))
		}

		r, err := svc.DescribeListeners(&elbv2.DescribeListenersInput{
			ListenerArns: arns,
		})
		if err != nil {
			return nil, err
		}

		for i := range r.Listeners {
			latestListenerInfo := r.Listeners[i]
			status := listenerStatuses[*latestListenerInfo.ListenerArn]
			status.Listener = latestListenerInfo
		}
	}

	sliceEq := func(t string, a, b []string) error {
		if len(a) != len(b) {
			return fmt.Errorf("slice length mismatch: got %d, want %d", len(a), len(b))
		}

		for i := range a {
			v1 := a[i]
			v2 := b[i]
			if v1 != v2 {
				return fmt.Errorf("non equal element at %d: got %v, want %v", i, v1, v2)
			}
		}

		return nil
	}

	copySortSlice := func(a []string) []string {
		copy := append([]string{}, a...)

		sort.Strings(copy)

		return copy
	}

	for _, l := range listenerStatuses {
		if len(l.ALBAttachments) > 1 {
			base := l.ALBAttachments[0]

			baseHosts := copySortSlice(base.Hosts)
			basePathPatterns := copySortSlice(base.PathPatterns)

			for i := 1; i < len(l.ALBAttachments); i++ {
				l2 := l.ALBAttachments[i]

				if l2.Protocol != base.Protocol {
					return nil, fmt.Errorf("validating alb attachment %d for listener %s: mismatching protocol: got %v for index %d, want %v", i, l.Listener.ListenerArn, l2.Priority, i, base.Priority)
				}

				if l2.Priority != base.Priority {
					return nil, fmt.Errorf("validating alb attachment %d for listener %s: mismatching priority: got %%v for index %d, want %v", i, l.Listener.ListenerArn, l2.Priority, i, base.Priority)
				}

				l2Hosts := copySortSlice(l2.Hosts)
				if err := sliceEq("hosts", l2Hosts, baseHosts); err != nil {
					return nil, fmt.Errorf("validating alb attachment %d for listener %s: mismatching hosts: index %d: %w", i, l.Listener.ListenerArn, i, err)
				}

				l2PathPatterns := copySortSlice(l2.PathPatterns)
				if err := sliceEq("path_patterns", l2PathPatterns, basePathPatterns); err != nil {
					return nil, fmt.Errorf("validating alb attachment %d for listener %s: mismatching path_patterns: index %d: %w", i, l.Listener.ListenerArn, i, err)
				}
			}
		}

		l.RulePriority = int64(l.ALBAttachments[0].Priority)
		l.Hosts = l.ALBAttachments[0].Hosts
		l.PathPatterns = l.ALBAttachments[0].PathPatterns
	}

	for listenerARN := range listenerStatuses {
		log.Printf("Reconciling listener %q: newId=%v, oldId=%v\n", listenerARN, newId, oldId)

		listenerStatus := listenerStatuses[listenerARN]

		if len(listenerStatus.ALBAttachments) > 1 {
			return nil, fmt.Errorf("only 1 ALB attachment per listener is currently supported")
		}

		a := listenerStatus.ALBAttachments[0]

		// We need to determine the current tg first.
		// Otherwise the desired and the current tg points to the same tg, which isn't what we want here.
		if oldId != "" {
			currentTGName := fmt.Sprintf("%s-%d-%s", a.NodeGroupName, a.NodePort, oldId)
			result, err := svc.DescribeTargetGroups(&elbv2.DescribeTargetGroupsInput{
				Names: []*string{aws.String(currentTGName)},
			})
			if err != nil {
				if aerr, ok := err.(awserr.Error); ok {
					switch aerr.Code() {
					case elbv2.ErrCodeLoadBalancerNotFoundException:
						fmt.Println(elbv2.ErrCodeLoadBalancerNotFoundException, aerr.Error())
					case elbv2.ErrCodeTargetGroupNotFoundException:
						fmt.Println(elbv2.ErrCodeTargetGroupNotFoundException, aerr.Error())
					default:
						fmt.Println(aerr.Error())
					}
				} else {
					// Print the error, cast err to awserr.Error to get the Code and
					// Message from an error.
					fmt.Println(err.Error())
				}
				return nil, err
			}
			listenerStatus.CurrentTG = result.TargetGroups[0]
		}

		if newId != "" {
			desiredTGName := fmt.Sprintf("%s-%d-%s", a.NodeGroupName, a.NodePort, newId)

			if len(desiredTGName) > 32 {
				return nil, fmt.Errorf("creating target group %s for cluster %s: target group name too long. it must be shorter than 33, but was %d", len(desiredTGName))
			}

			var targetType string

			if a.NodePort != 0 {
				targetType = "instance"
			} else {
				return nil, fmt.Errorf("BUG: alb_attachment.node_port cannot be omitted yet: %v", a)
			}

			createTgInput := &elbv2.CreateTargetGroupInput{
				Name:       aws.String(desiredTGName),
				Port:       aws.Int64(int64(a.NodePort)),
				TargetType: aws.String(targetType),
				VpcId:      aws.String(cluster.VPCID),
				Protocol:   aws.String(strings.ToUpper(a.Protocol)),
			}
			created, err := svc.CreateTargetGroup(createTgInput)
			if err != nil {
				log.Printf("creating target group with input: %+v", createTgInput)

				return nil, fmt.Errorf("creating target group %s for cluster %s: %w", desiredTGName, cluster.Name, err)
			}

			ec2svc := ec2.New(awsclicompat.NewSession(cluster.Region))
			ec2svc.CreateTags(&ec2.CreateTagsInput{
				Resources: nil,
				Tags: []*ec2.Tag{
					{
						Key:   aws.String("tf-eksctl/node-group"),
						Value: aws.String(a.NodeGroupName),
					},
					{
						Key:   aws.String("tf-eksctl/cluster"),
						Value: aws.String(cluster.Name),
					},
				},
			})

			listenerStatus.DesiredTG = created.TargetGroups[0]
		}

		r, err := svc.DescribeRules(&elbv2.DescribeRulesInput{
			ListenerArn: aws.String(listenerARN),
		})
		if err != nil {
			return nil, err
		}

		var targetRuleBeforeUpdate *elbv2.Rule

		log.Printf("determining if listener rule needs to be created: tg desired=%+v, current=%+v, rules=%+v", listenerStatus.DesiredTG, listenerStatus.CurrentTG, r.Rules)

		if len(r.Rules) == 0 {
			if listenerStatus.DesiredTG == nil {
				return nil, fmt.Errorf("unsupported case: no listener rule to create")
			}
		} else if listenerStatus.DesiredTG != nil && listenerStatus.CurrentTG != nil {
		RULES:
			for i := range r.Rules {
				r := r.Rules[i]
				// Find the specific rule and set it to target rule
				if len(r.Actions) > 0 && r.Actions[0].ForwardConfig != nil && len(r.Actions[0].ForwardConfig.TargetGroups) > 0 {
					for _, tg := range r.Actions[0].ForwardConfig.TargetGroups {
						primaryTGName := *tg.TargetGroupArn
						primaryTGNameFromTFState := *listenerStatus.CurrentTG.TargetGroupArn
						if primaryTGName == primaryTGNameFromTFState {
							targetRuleBeforeUpdate = r
							break RULES
						}
					}
				}
			}
		}

		var targetRuleAfterUpdate *elbv2.Rule
		{
			if targetRuleBeforeUpdate == nil && listenerStatus.DesiredTG != nil {
				// Create rule and set it to l.Rule
				ruleConditions := []*elbv2.RuleCondition{
					//	{
					//		Field:                   nil,
					//		HostHeaderConfig:        nil,
					//		HttpHeaderConfig:        nil,
					//		HttpRequestMethodConfig: nil,
					//		PathPatternConfig:       nil,
					//		QueryStringConfig:       nil,
					//		SourceIpConfig:          nil,
					//		Values:                  nil,
					//	}
				}

				if len(listenerStatus.Hosts) > 0 {
					ruleConditions = append(ruleConditions, &elbv2.RuleCondition{
						Field: aws.String("host-header"),
						HostHeaderConfig: &elbv2.HostHeaderConditionConfig{
							Values: aws.StringSlice(listenerStatus.Hosts),
						},
					})
				}

				if len(listenerStatus.PathPatterns) > 0 {
					ruleConditions = append(ruleConditions, &elbv2.RuleCondition{
						Field: aws.String("path-pattern"),
						PathPatternConfig: &elbv2.PathPatternConditionConfig{
							Values: aws.StringSlice(listenerStatus.PathPatterns),
						},
					})
				}

				createRuleInput := &elbv2.CreateRuleInput{
					Actions: []*elbv2.Action{
						{
							ForwardConfig: &elbv2.ForwardActionConfig{
								TargetGroupStickinessConfig: nil,
								TargetGroups: []*elbv2.TargetGroupTuple{
									{
										TargetGroupArn: listenerStatus.DesiredTG.TargetGroupArn,
										Weight:         aws.Int64(100),
									},
								},
							},
							TargetGroupArn: listenerStatus.DesiredTG.TargetGroupArn,
							Type:           aws.String("forward"),
						},
					},
					Priority:    aws.Int64(listenerStatus.RulePriority),
					Conditions:  ruleConditions,
					ListenerArn: aws.String(listenerARN),
				}
				created, err := svc.CreateRule(createRuleInput)
				if err != nil {
					log.Printf("creating rule: %+v", createRuleInput)

					return nil, fmt.Errorf("creating listener rule for listener %s: %w", listenerARN, err)
				}
				targetRuleAfterUpdate = created.Rules[0]
			} else if targetRuleBeforeUpdate != nil && listenerStatus.DesiredTG != nil && listenerStatus.CurrentTG != nil {
				modifyRuleInput := &elbv2.ModifyRuleInput{
					Actions: []*elbv2.Action{
						{
							ForwardConfig: &elbv2.ForwardActionConfig{
								TargetGroupStickinessConfig: nil,
								TargetGroups: []*elbv2.TargetGroupTuple{
									{
										TargetGroupArn: listenerStatus.DesiredTG.TargetGroupArn,
										Weight:         aws.Int64(0),
									}, {
										TargetGroupArn: listenerStatus.CurrentTG.TargetGroupArn,
										Weight:         aws.Int64(100),
									},
								},
							},
							Type: aws.String("forward"),
						},
					},
					RuleArn: targetRuleBeforeUpdate.RuleArn,
				}
				updated, err := svc.ModifyRule(modifyRuleInput)
				if err != nil {
					log.Printf("modifying rule: %+v", modifyRuleInput)

					return nil, err
				}
				targetRuleAfterUpdate = updated.Rules[0]
			}
		}
		listenerStatus.Rule = targetRuleAfterUpdate
	}

	r := ListenerStatuses{}
	for k, v := range listenerStatuses {
		r[k] = *v
	}

	return r, nil
}

type CanaryOpts struct {
	CanaryAdvancementInterval time.Duration
	CanaryAdvancementStep     int
}

func graduallyShiftTraffic(set *ClusterSet, opts CanaryOpts) error {
	svc := elbv2.New(awsclicompat.NewSession(set.Cluster.Region))

	listenerStatuses := set.ListenerStatuses

	setDesiredTGTrafficPercentage := func(l ListenerStatus, p int) error {
		if p > 100 {
			return fmt.Errorf("BUG: invalid value for p: got %d, must be less than 100", p)
		}

		if l.DesiredTG == nil {
			return fmt.Errorf("BUG: DesiredTG is nil: %+v", l)
		}

		if l.CurrentTG == nil {
			return fmt.Errorf("BUG: CurrentTG is nil: %+v", l)
		}

		if l.Rule == nil {
			return fmt.Errorf("BUG: Rule is nil: %+v", l)
		}

		_, err := svc.ModifyRule(&elbv2.ModifyRuleInput{
			Actions: []*elbv2.Action{
				{
					ForwardConfig: &elbv2.ForwardActionConfig{
						TargetGroupStickinessConfig: nil,
						TargetGroups: []*elbv2.TargetGroupTuple{
							{
								TargetGroupArn: l.DesiredTG.TargetGroupArn,
								Weight:         aws.Int64(int64(p)),
							}, {
								TargetGroupArn: l.CurrentTG.TargetGroupArn,
								Weight:         aws.Int64(int64(100 - p)),
							},
						},
					},
					Order: aws.Int64(1),
					Type:  aws.String("forward"),
				},
			},
			RuleArn: l.Rule.RuleArn,
		})
		if err != nil {
			return err
		}

		return nil
	}

	for _, l := range listenerStatuses {
		if l.Rule.Actions != nil && len(l.Rule.Actions) > 0 {
			if len(l.Rule.Actions) != 1 {
				return fmt.Errorf("unexpected number of actions in rule %q: want 2, got %d", *l.Rule.RuleArn, len(l.Rule.Actions))
			}

			// Gradually shift traffic from current tg to desired tg by
			// updating rule
			var step int

			if opts.CanaryAdvancementStep > 0 {
				step = opts.CanaryAdvancementStep
			} else {
				step = 5
			}

			for p := 1; p < 100; p += step {
				if err := setDesiredTGTrafficPercentage(l, p); err != nil {
					return err
				}

				if opts.CanaryAdvancementInterval != 0 {
					time.Sleep(opts.CanaryAdvancementInterval)
				}
			}

			if err := setDesiredTGTrafficPercentage(l, 100); err != nil {
				return err
			}
		}
	}

	return nil
}