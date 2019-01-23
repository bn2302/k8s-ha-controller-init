package cmd

import (
	"errors"
	"io"
	"net"
	"sort"
	"strconv"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/ec2metadata"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/autoscaling"
	"github.com/aws/aws-sdk-go/service/autoscaling/autoscalingiface"
	"github.com/spf13/cobra"
)

var caKeys = map[string]string{
	"admin.conf":         "/etc/kubernetes/admin.conf",
	"ca.crt":             "/etc/kubernetes/pki/ca.crt",
	"ca.key":             "/etc/kubernetes/pki/ca.key",
	"etcd-ca.crt":        "/etc/kubernetes/pki/etcd/ca.crt",
	"etcd-ca.key":        "/etc/kubernetes/pki/etcd/ca.key",
	"front-proxy-ca.crt": "/etc/kubernetes/pki/front-proxy-ca.crt",
	"front-proxy-ca.key": "/etc/kubernetes/pki/front-proxy-ca.key",
	"sa.key":             "/etc/kubernetes/pki/sa.key",
	"sa.pub":             "/etc/kubernetes/pki/sa.pub",
}

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

func kubeUp(apiDNS string, apiPort int) bool {
	retry := 0
	for {
		_, err := net.Dial("tcp", apiDNS+":"+strconv.Itoa(apiPort))
		if err != nil {
			if retry > 3 {
				return false
			}
			retry += 1
			time.Sleep(time.Second * 0.1)
		}
		return true
	}
}

func caExistsOnS3(svc *s3iface.S3API, bucket string) {

	resp, err := svc.ListObjects(&s3.ListObjectsInput{Bucket: aws.String(bucket)})
	if err != nil {
		exitErrorf("Unable to list items in bucket %q, %v", bucket, err)
	}
	mapObj := map[string]bool{}
	for _, item := range resp.Contents {
		mapObj[*item.Key] = true
	}

	for k, _ := range caKeys {
		if _, ok := mapObj[k]; !ok {
			return false
		}
	}

	return true
}

func downloadCAFromS3(svc *s3iface.S3API, bucket string) {
	exec.Cmd("mkdir -p /etc/kubernetes/pki/etcd")
	for k, p := range caKeus {
		result, _ := svc.GetObject(
			&s3.GetObjectInput{
				Bucket: aws.String(bucket),
				Key:    aws.String(k),
			},
		)
		file, _ := os.file(p)
		file.Write(*result.Object)
	}
}

func uploadCAToS3(svc *s3iface.S3API, bucket string) {
	for k, p := range caKeus {
		file, _ := os.file(p)
		result, _ := svc.PutObject(
			&s3.PutObjectInput{
				Body:   aws.ReadSeekCloser(file.Read()),
				Bucket: aws.String(bucket),
				Key:    aws.String(k),
			},
		)
	}
}

func runInitController(bucket string) {
	exec.Cmd("kubeadm init --config=kubeadm-config.yaml")
	exec.Cmd("kubectl apply -f \"https://cloud.weave.works/k8s/net?k8s-version=$(kubectl version | base64 | tr -d '\n')\"")

}

func runJoinController(bucket string) {
	exec.Cmd("kubeadm join 192.168.0.200:6443 --token j04n3m.octy8zely83cy2ts --discovery-token-ca-cert-hash sha256:84938d2a22203a8e56a787ec0c6ddad7bc7dbd52ebabc62fd5f4dbea72b14d1f --experimental-control-plane")
}

func deployController(stdout io.Writer) {
	apiDNS := ""
	bucket := ""
	apiPort := 6443
	sess, _ := session.NewSession()
	metaSvc := ec2metadata.New(sess)
	instanceID, _ := getInstanceID(metaSvc)
	region, _ := getRegion(metaSvc)
	autoSvc := autoscaling.New(sess, aws.NewConfig().WithRegion(region))
	s3Svc := s3.New(sess, aws.NewConfig().WithRegion(region))
	groupName, _ := getAutoscalingGroupName(autoSvc, instanceID)
	group, _ := getAutoscalingGroup(autoSvc, groupName)
	for {
		if !kubeUp(apiDNS, apiPort) {
			_ = waitTillCapacitypReached(group, 600)
			if instanceID == getAutoscalingInstances(group)[0] {
				if caExistsOnS3(bucket) {
					_ := downloadCAFromS3(bucket)
				}
				runInitController(bucket)
				if !caExistsOnS3(bucket) {
					uploadCAToS3(bucket)
				}
				return
			}
		}
		if kubeUp(apiDNS, apiPort) & caExistsOnS3(bucket) {
			_ := downloadCAFromS3(bucket)
			runJoinController(bucket)
			return
		}
		time.Sleep(time.Second * 1)
	}
}

func deployWorker(stdout io.Writer) {

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
