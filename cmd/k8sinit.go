package cmd

import (
	"errors"
	"io"
	"net/http"
	"sort"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/ec2metadata"
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

func waitTillCapacitypReached(group *autoscaling.Group) (bool, error) {
	for {
		if int64(len(group.Instances)) == *group.DesiredCapacity {
			return true, nil
		}
		time.Sleep(time.Second * 30)
	}
	return false, errors.New("AutoScalingGroup did not reach capacity")
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

func runInit(cfgpth string) {
}

func runJoin(token string, cahash string) {
}

func setupCluster(stdout io.Writer) {
	//	cfgpth := "./kubeadm-config.yaml"
	//	apiDNS := "apidns"
	//	instance := getInstanceID()
	//	instances := getInstancesInAutoScalingGroup()
	//	for {
	//		if kubeUp(apiDNS) {
	//			break
	//		}
	//		if instances[0] == instance && !kubeUp(apiDNS) {
	//			runInit(cfgpth)
	//			//			copyCA(s3bkt)
	//			return
	//		}
	//		time.Sleep(time.Second * 60)
	//	}
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
			setupCluster(stdout)
		},
	}
	cmd.AddCommand(controllerCmd)

	return cmd
}
