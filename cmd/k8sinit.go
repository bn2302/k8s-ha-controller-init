package cmd

import (
	"errors"
	"io"
	"net/http"
	"sort"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/ec2metadata"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/autoscaling"
	"github.com/aws/aws-sdk-go/service/autoscaling/autoscalingiface"
	"github.com/spf13/cobra"
)

func getInstanceID(svc *ec2metadata.EC2Metadata) (string, error) {
	doc, err := svc.GetInstanceIdentityDocument()
	if err != nil {
		return "", err
	}
	return doc.InstanceID, nil
}

func getRegion(svc *ec2metadata.EC2Metadata) (string, error) {
	doc, err := svc.GetInstanceIdentityDocument()
	if err != nil {
		return "", err
	}
	return doc.Region, nil
}

func getAutoscalingGroupName(svc autoscalingiface.AutoScalingAPI, instanceID string) (string, error) {
	autoInstance, err := svc.DescribeAutoScalingInstances(
		&autoscaling.DescribeAutoScalingInstancesInput{
			InstanceIds: []*string{
				aws.String(instanceID),
			},
		},
	)
	if err != nil {
		return "", err
	}
	return *autoInstance.AutoScalingInstances[0].AutoScalingGroupName, nil
}

func getAutoscalingGroup(svc autoscalingiface.AutoScalingAPI, groupName string) (*autoscaling.Group, error) {
	groups, err := svc.DescribeAutoScalingGroups(
		&autoscaling.DescribeAutoScalingGroupsInput{
			AutoScalingGroupNames: []*string{
				aws.String(groupName),
			},
		},
	)
	if err != nil {
		return nil, err
	}
	return groups.AutoScalingGroups[0], nil
}

func waitTillCapacitypReached(group *autoscaling.Group, timeout time.Duration) error {

	c1 := make(chan bool, 1)
	go func() {
		for {
			if int64(len(group.Instances)) == *group.DesiredCapacity {
				c1 <- true
				break
			}
			time.Sleep(time.Second * 5)
		}
	}()
	select {
	case <-c1:
		return nil
	case <-time.After(timeout * time.Second):
		return errors.New("AutoScalingGroup did not reach capacity")
	}
}

func getAutoscalingInstances(group *autoscaling.Group) []string {
	instances := make([]string, len(group.Instances))
	for i, v := range group.Instances {
		instances[i] = *v.InstanceId
	}
	sort.Strings(instances)
	return instances
}

func kubeUp(apiDNS string) bool {
	_, err := http.Get("https://" + apiDNS + "/version")
	if err != nil {
		return false
	}
	return true
}

func runInitController(bucket string) {
}

func runJoinController(bucket string) {
}

func deployController(stdout io.Writer) {
	apiDNS := ""
	bucket := ""
	sess, _ := session.NewSession()
	metaSvc := ec2metadata.New(sess)
	instanceID, _ := getInstanceID(metaSvc)
	region, _ := getRegion(metaSvc)
	autoSvc := autoscaling.New(sess, aws.NewConfig().WithRegion(region))
	groupName, _ := getAutoscalingGroupName(autoSvc, instanceID)
	group, _ := getAutoscalingGroup(autoSvc, groupName)
	for {
		if !kubeUp(apiDNS) {
			_ = waitTillCapacitypReached(group, 600)
			if instanceID == getAutoscalingInstances(group)[0] {
				runInitController(bucket)
				return
			}
		}
		if kubeUp(apiDNS) {
			runJoinController(bucket)
			return
		}
		time.Sleep(time.Second * 1)
	}
}

// NewHAKubeadm returns a ha kubeadm command
func NewHAKubeadm(stdout io.Writer) *cobra.Command {

	cmd := &cobra.Command{
		Use:           "k8sinit",
		Short:         "Deploy a HA kubernetes",
		Long:          `Initialize a kubernetes HA cluster using kubeadm on AWS`,
		SilenceErrors: true,
	}

	controllerCmd := &cobra.Command{
		Use:   "controller",
		Short: "Deploy controller",
		Long:  `Deploys a HA controller on AWS.`,
		Run: func(cmd *cobra.Command, args []string) {
			deployController(stdout)
		},
	}
	cmd.AddCommand(controllerCmd)

	return cmd
}
