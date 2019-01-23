package pkg

import (
	"bytes"
	"errors"
	"io"
	"io/ioutil"
	"net"
	"os"
	"sort"
	"strconv"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/ec2metadata"
	"github.com/aws/aws-sdk-go/service/autoscaling"
	"github.com/aws/aws-sdk-go/service/autoscaling/autoscalingiface"
	"github.com/aws/aws-sdk-go/service/s3"
	"github.com/aws/aws-sdk-go/service/s3/s3iface"
)

//GetInstanceID returns the EC2 instance name
func GetInstanceID(svc *ec2metadata.EC2Metadata) (string, error) {
	doc, err := svc.GetInstanceIdentityDocument()
	if err != nil {
		return "", err
	}
	return doc.InstanceID, nil
}

//GetRegion returns the region the instance is running in
func GetRegion(svc *ec2metadata.EC2Metadata) (string, error) {
	doc, err := svc.GetInstanceIdentityDocument()
	if err != nil {
		return "", err
	}
	return doc.Region, nil
}

//GetAutoscalingGroupName gets the autoscaling group name the instance is belonging to
func GetAutoscalingGroupName(svc autoscalingiface.AutoScalingAPI, instanceID string) (string, error) {
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

//GetAutoscalingGroup gets the autoscaling group of the instance from the name of the autoscaling group
func GetAutoscalingGroup(svc autoscalingiface.AutoScalingAPI, groupName string) (*autoscaling.Group, error) {
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

//WaitTillCapacityReached waits until the autoscaling group is up and running
func WaitTillCapacityReached(group *autoscaling.Group, timeout time.Duration) error {

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

//GetAutoscalingInstances gets all instances in an autoscaling group
func GetAutoscalingInstances(group *autoscaling.Group) []string {
	instances := make([]string, len(group.Instances))
	for i, v := range group.Instances {
		instances[i] = *v.InstanceId
	}
	sort.Strings(instances)
	return instances
}

//KubeUp checks if kubernetes is running
func KubeUp(apiDNS string, apiPort int) bool {
	retry := 0
	for {
		_, err := net.Dial("tcp", apiDNS+":"+strconv.Itoa(apiPort))
		if err == nil {
			return true
		}
		if retry > 2 {
			return false
		}
		retry++
		time.Sleep(time.Millisecond * 100)
	}
}

//CaExistsOnS3 determines if the kube pki is on s3
func ExistsOnS3(svc s3iface.S3API, bucket string, keyPath *map[string]string) bool {

	resp, _ := svc.ListObjects(&s3.ListObjectsInput{Bucket: aws.String(bucket)})

	mapObj := map[string]bool{}
	for _, item := range resp.Contents {
		mapObj[*item.Key] = true
	}

	for f := range *keyPath {
		if _, ok := mapObj[f]; !ok {
			return false
		}
	}

	return true
}

//DownloadCAFromS3 gets the kube pki from s3
func DownloadFromS3(svc s3iface.S3API, bucket string, keyPath *map[string]string) {
	for k, p := range *keyPath {
		result, _ := svc.GetObject(
			&s3.GetObjectInput{
				Bucket: aws.String(bucket),
				Key:    aws.String(k),
			},
		)
		outfile, _ := os.Create(p)
		io.Copy(outfile, result.Body)
		outfile.Close()
	}
}

//UploadCAToS3 puts the kube pki on s3
func UploadToS3(svc s3iface.S3API, bucket string, keyPath *map[string]string) {
	for k, p := range *keyPath {
		dat, _ := ioutil.ReadFile(p)
		svc.PutObject(
			&s3.PutObjectInput{
				Body:   bytes.NewReader(dat),
				Bucket: aws.String(bucket),
				Key:    aws.String(k),
			},
		)
	}
}
