package pkg

import (
	"net"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/ec2metadata"
	"github.com/aws/aws-sdk-go/awstesting/unit"
	"github.com/aws/aws-sdk-go/service/autoscaling"
	"github.com/aws/aws-sdk-go/service/autoscaling/autoscalingiface"
)

const instanceIdentityDocument = `{
	"devpayProductCodes" : null,
	"availabilityZone" : "eu-west-1a",
	"privateIp" : "10.240.0.10",
	"version" : "2010-08-31",
	"region" : "eu-west-1",
	"instanceId" : "i-9242867120lbndef1",
	"billingProducts" : null,
	"instanceType" : "t3.small",
	"accountId" : "123456789012",
	"pendingTime" : "2018-11-19T16:32:11Z",
	"imageId" : "ami-12345678",
	"kernelId" : "aki-12345678",
	"ramdiskId" : null,
	"architecture" : "x86_64"
}`

func stringAddress(str string) *string {
	return &str
}

func initTestServer(path string, resp string) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.RequestURI != path {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}

		w.Write([]byte(resp))
	}))
}

type mockAutoScalingClient struct {
	autoscalingiface.AutoScalingAPI
	describeAutoScalingInstancesOutput *autoscaling.DescribeAutoScalingInstancesOutput
	describeAutoScalingGroupsOutput    *autoscaling.DescribeAutoScalingGroupsOutput
}

func newMockAutoScalingClient() *mockAutoScalingClient {
	c := mockAutoScalingClient{}
	c.describeAutoScalingInstancesOutput = &autoscaling.DescribeAutoScalingInstancesOutput{
		AutoScalingInstances: []*autoscaling.InstanceDetails{
			{
				AutoScalingGroupName: stringAddress("auto-test-group"),
			},
		},
		NextToken: stringAddress(""),
	}
	c.describeAutoScalingGroupsOutput = &autoscaling.DescribeAutoScalingGroupsOutput{
		AutoScalingGroups: []*autoscaling.Group{
			{
				Instances: []*autoscaling.Instance{
					{
						InstanceId: stringAddress("i-143adsf"),
					},
					{
						InstanceId: stringAddress("i-423adsf"),
					},
					{
						InstanceId: stringAddress("i-143ads4"),
					},
				},
			},
		},
		NextToken: stringAddress(""),
	}
	return &c
}

func (m *mockAutoScalingClient) DescribeAutoScalingInstances(input *autoscaling.DescribeAutoScalingInstancesInput) (*autoscaling.DescribeAutoScalingInstancesOutput, error) {

	return m.describeAutoScalingInstancesOutput, nil

}

func (m *mockAutoScalingClient) DescribeAutoScalingGroups(input *autoscaling.DescribeAutoScalingGroupsInput) (*autoscaling.DescribeAutoScalingGroupsOutput, error) {
	return m.describeAutoScalingGroupsOutput, nil
}

func TestGetInstanceID(t *testing.T) {
	server := initTestServer(
		"/latest/dynamic/instance-identity/document",
		instanceIdentityDocument,
	)
	defer server.Close()
	c := ec2metadata.New(unit.Session, &aws.Config{Endpoint: aws.String(server.URL + "/latest")})
	id, err := GetInstanceID(c)
	if err != nil {
		t.Errorf("expect no error, got %v", err)
	}
	if e, a := id, "i-9242867120lbndef1"; e != a {
		t.Errorf("expect %v, got %v", e, a)
	}
}
func TestGetRegion(t *testing.T) {
	server := initTestServer(
		"/latest/dynamic/instance-identity/document",
		instanceIdentityDocument,
	)
	defer server.Close()
	c := ec2metadata.New(unit.Session, &aws.Config{Endpoint: aws.String(server.URL + "/latest")})
	id, err := GetRegion(c)
	if err != nil {
		t.Errorf("expect no error, got %v", err)
	}
	if e, a := id, "eu-west-1"; e != a {
		t.Errorf("expect %v, got %v", e, a)
	}
}

func TestGetAutoscalingGroupName(t *testing.T) {
	mockSvc := newMockAutoScalingClient()
	groupName, err := GetAutoscalingGroupName(mockSvc, "i-9242867120lbndef1")
	if err != nil {
		t.Errorf("expect no error, got %v", err)
	}
	if e, a := groupName, "auto-test-group"; e != a {
		t.Errorf("expect %v, got %v", e, a)
	}
}

func TestGetAutoscalingGroup(t *testing.T) {
	mockSvc := newMockAutoScalingClient()
	_, err := GetAutoscalingGroup(mockSvc, "")
	if err != nil {
		t.Errorf("expect no error, got %v", err)
	}
}

func TestGetAutoscalingInstances(t *testing.T) {
	mockSvc := newMockAutoScalingClient()
	group, _ := GetAutoscalingGroup(mockSvc, "")
	instances := GetAutoscalingInstances(group)
	if e, a := instances, []string{"i-143ads4", "i-143adsf", "i-423adsf"}; !reflect.DeepEqual(e, a) {
		t.Errorf("expect %v, got %v", e, a)
	}

}

func increaseInstances(group *autoscaling.Group) {
	time.Sleep(time.Second * 10)
	group.Instances = append(
		group.Instances,
		&autoscaling.Instance{
			InstanceId: stringAddress("i-543ads4"),
		},
	)
}

func TestWaitTillCapacitypReached(t *testing.T) {
	mockSvc := newMockAutoScalingClient()
	group, _ := GetAutoscalingGroup(mockSvc, "")
	capacity := int64(4)
	group.DesiredCapacity = &capacity
	go increaseInstances(group)
	err := WaitTillCapacityReached(group, 1)
	if err == nil {
		t.Errorf("expect error, got %v", err)
	}
	err = WaitTillCapacityReached(group, 15)
	if err != nil {
		t.Errorf("expect no error, got %v", err)
	}
}

func TestKubeUp(t *testing.T) {
	if KubeUp("", 6443) {
		t.Errorf("expect no error, tcp server should be down")
	}
	l, err := net.Listen("tcp", ":6443")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	if !KubeUp("", 6443) {
		t.Errorf("expect no error, tcp server should be up")
	}
}
