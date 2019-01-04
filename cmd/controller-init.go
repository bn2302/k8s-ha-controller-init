package main

import (
	"fmt"
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/ec2metadata"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/autoscaling"
	kubeadmapiv1beta1 "k8s.io/kubernetes/cmd/kubeadm/app/apis/kubeadm/v1beta1"
	"k8s.io/kubernetes/cmd/kubeadm/app/cmd"
	configutil "k8s.io/kubernetes/cmd/kubeadm/app/util/config"
	"log"
	"net/http"
	"os"
	"sort"
	"time"
)

func getInstanceID() string {
	sess, err := session.NewSession()
	if err != nil {
		log.Fatal("Creating session is failed")
	}
	instanceIdentityDocument, err := ec2metadata.New(sess).GetInstanceIdentityDocument()
	if err != nil {
		log.Fatal("Getting Instance Metadata failed")
	}
	return instanceIdentityDocument.InstanceID
}

func getInstancesInAutoScalingGroup() []string {
	sess, err := session.NewSession()
	if err != nil {
		log.Fatal("Creating session is failed")
	}
	instanceIdentityDocument, err := ec2metadata.New(sess).GetInstanceIdentityDocument()
	if err != nil {
		log.Fatal("Getting Instance Metadata failed")
	}
	instanceID := getInstanceID()
	region := instanceIdentityDocument.Region
	autoSvc := autoscaling.New(sess, aws.NewConfig().WithRegion(region))
	autoInstance, err := autoSvc.DescribeAutoScalingInstances(
		&autoscaling.DescribeAutoScalingInstancesInput{
			InstanceIds: []*string{
				aws.String(instanceID),
			},
		},
	)
	if err != nil {
		log.Fatal("Getting Autoscaling Group of Instance failed")
	}
	for {
		autoGroupRes, err := autoSvc.DescribeAutoScalingGroups(&autoscaling.DescribeAutoScalingGroupsInput{
			AutoScalingGroupNames: []*string{
				aws.String(*autoInstance.AutoScalingInstances[0].AutoScalingGroupName),
			},
		},
		)
		if err != nil {
			log.Fatal("Getting Autoscaling Group failed")
		}
		autoGroup := autoGroupRes.AutoScalingGroups[0]
		if int64(len(autoGroup.Instances)) == *autoGroup.DesiredCapacity {
			instances := make([]string, len(autoGroup.Instances))
			for i, v := range autoGroup.Instances {
				instances[i] = *v.InstanceId
			}
			sort.Strings(instances)
			return instances
		}
		time.Sleep(time.Second * 30)
	}
	return nil
}

func getAPIServerDNS(cfgpth *string) *string {
	externalcfg := &kubeadmapiv1beta1.InitConfiguration{}
	cfg, err := configutil.ConfigFileAndDefaultsToInternalConfig(cfgpth, externalcfg)
	if err != nil {
		log.Fatal("Cannot Read Config file")
	}
	return cfg.ControlPlaneEndpoint
}

func checkIfKubeIsUP(apiDNS *string) bool {
	req, err := http.Get("https://" + cfg.ControlPlaneEndpoint + "/version")
	if err != nil {
		fmt.Println(err)
		return false
	}
	fmt.Println(req)
	return true
}

func runInit(cfgpth *string) {
	in := cmd.NewCmdInit(os.Stdout)
	args := []string{"--skip-phases", "preflight", "--config", cfgpth}
	in.SetArgs(args)
	in.Execute()
}

func runJoin(token *string, cahash *string) {
	in := cmd.NewCmdInit(os.Stdout)
	args := []string{"--token", "preflight", "--config", cfgpth}
	in.SetArgs(args)
	in.Execute()
}

func main() {
	cfgpth := "./kubeadm-config.yaml"
	instance := getInstanceID()
	instances := getInstancesInAutoScalingGroup()
	apiDNS := getAPIServerDNS(cfgpth)
	for {
		if kubeUp(apiDNS) {
			break
		}
		if instances[0] == instance && !kubeUp(apiDNS) {
			runInit(cfgpth)
			copyCA(s3bkt)
			return
		}
		time.Sleep(time.Second * 60)
	}
	getCAFromS3()
	getTokenFromS3()
	calcCAhash()
	runJoin(token, cahash)

}
