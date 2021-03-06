package pkg

import (
	"bytes"
	"errors"
	"io"
	"io/ioutil"
	"log"
	"os"
	"sort"
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

//ExistsOnS3 determines if the kube pki is on s3
func ExistsOnS3(svc s3iface.S3API, bucket string, keyPath *map[string]string) (bool, error) {

	resp, err := svc.ListObjects(&s3.ListObjectsInput{Bucket: aws.String(bucket)})
	if err != nil {
		return false, err
	}

	mapObj := map[string]bool{}
	for _, item := range resp.Contents {
		mapObj[*item.Key] = true
	}

	for f := range *keyPath {
		if _, ok := mapObj[f]; !ok {
			return false, nil
		}
	}

	return true, nil
}

//DownloadMapFromS3 gets a map describing keys from s3 and downloads them to a path
func DownloadMapFromS3(svc s3iface.S3API, bucket string, keyPath *map[string]string) error {
	for k, p := range *keyPath {
		err := DownloadFromS3(svc, bucket, k, p)
		if err != nil {
			return err
		}
	}
	return nil
}

//DownloadFromS3 gets the kube pki from s3
func DownloadFromS3(svc s3iface.S3API, bucket string, key string, path string) error {
	result, err := svc.GetObject(
		&s3.GetObjectInput{
			Bucket: aws.String(bucket),
			Key:    aws.String(key),
		},
	)
	if err != nil {
		return err
	}
	outfile, err := os.Create(path)
	if err != nil {
		return err
	}
	defer func() {
		err := outfile.Close()
		if err != nil {
			log.Fatal(err)
		}
	}()
	_, err = io.Copy(outfile, result.Body)
	if err != nil {
		return err
	}
	return nil
}

//UploadMapToS3 puts a map of files to S3
func UploadMapToS3(svc s3iface.S3API, bucket string, keyPath *map[string]string) error {
	for k, p := range *keyPath {
		err := UploadToS3(svc, bucket, k, p)
		if err != nil {
			return err
		}
	}
	return nil
}

//UploadToS3 puts a file to s3
func UploadToS3(svc s3iface.S3API, bucket string, key string, path string) error {
	dat, err := ioutil.ReadFile(path)
	if err != nil {
		return err
	}
	if _, err := svc.PutObject(&s3.PutObjectInput{Body: bytes.NewReader(dat), Bucket: aws.String(bucket), Key: aws.String(key)}); err != nil {
		return err
	}

	return nil
}
