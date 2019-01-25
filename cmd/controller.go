package cmd

import (
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/ec2metadata"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/autoscaling"
	"github.com/aws/aws-sdk-go/service/s3"
	"github.com/aws/aws-sdk-go/service/s3/s3iface"
	"github.com/bn2302/k8s-ha-controller-init/pkg"
	"github.com/spf13/cobra"
	"io/ioutil"
	"log"
	"os"
	"os/exec"
	"strconv"
	"time"
)

func createController() {
	exec.Command(
		"kubeadm",
		"init",
		"--config",
		clusterConfig["kubeadm-cfg-init.yaml"],
	).Run()
	exec.Command(
		"kubectl",
		"apply",
		"-f",
		"\"https://cloud.weave.works/k8s/net?k8s-version=$(kubectl version | base64 | tr -d '\n')\"",
	).Run()
	clusterInfoData, _ := exec.Command(
		"kubectl",
		"--kubeconfig=/etc/kubernetes/admin.conf",
		"get",
		"cm",
		"-n",
		"kube-public",
		"cluster-info",
		"-o",
		"jsonpath={.data.kubeconfig}",
	).Output()
	ioutil.WriteFile(clusterConfig["cluster-info.yaml"], clusterInfoData, 0644)
}

func joinController(svc s3iface.S3API, apiDNS string, apiPort int, bucket string) {
	os.MkdirAll("/etc/kubernetes/pki/etcd", 0777)
	pkg.DownloadFromS3(svc, bucket, "cluster-info.yaml", clusterConfig["cluster-info.yaml"])
	pkg.DownloadFromS3(svc, bucket, "kubeadm-cfg-join.yaml", clusterConfig["kubeadm-cfg-join.yaml"])
	pkg.DownloadMapFromS3(svc, bucket, &caKeys)
	exec.Command(
		"kubeadm",
		"join",
		apiDNS+":"+strconv.Itoa(apiPort),
		"--config",
		clusterConfig["kubeadm-cfg-join.yaml"],
		"--experimental-control-plane",
	).Run()
}

func initController(svc s3iface.S3API, bucket string) {
	if val, serr := pkg.ExistsOnS3(svc, bucket, &caKeys); serr != nil {
		log.Fatalln("Could not check if package exists: " + serr.Error())
	} else if val {
		merr := os.MkdirAll("/etc/kubernetes/pki/etcd", 0777)
		if merr != nil {
			log.Fatalln("Could not create directory : " + merr.Error())
		}
		derr := pkg.DownloadMapFromS3(svc, bucket, &caKeys)
		if derr != nil {
			log.Fatalln("Download create directory : " + derr.Error())
		}
	}
	derr := pkg.DownloadFromS3(svc, bucket, "kubeadm-cfg-init.yaml", clusterConfig["kubeadm-cfg-init.yaml"])
	if derr != nil {
		log.Fatalln("Could not download from S3 : " + derr.Error())
	}
	createController()
	if val, serr := pkg.ExistsOnS3(svc, bucket, &caKeys); serr != nil {
		log.Fatalln("Could not check if package exists: " + serr.Error())
	} else if !val {
		uerr := pkg.UploadMapToS3(svc, bucket, &caKeys)
		if uerr != nil {
			log.Fatalln("Could not upload pki to S3 : " + uerr.Error())
		}
	}

	uerr := pkg.UploadToS3(svc, bucket, "cluster-info.yaml", clusterConfig["cluster-info.yaml"])
	if uerr != nil {
		log.Fatalln("Could not upload clouser info to S3 : " + uerr.Error())

	}
}

func deployController(apiDNS string, apiPort int, bucket string) {
	sess, err := session.NewSession()
	if err != nil {
		log.Fatalln("Could not initialize aws session: " + err.Error())
	} else {
		log.Println("AWS Session started")
	}
	metaSvc := ec2metadata.New(sess)
	instanceID, err := pkg.GetInstanceID(metaSvc)
	if err != nil {
		log.Fatalln("Could not get instance id: " + err.Error())
	} else {
		log.Println("Got instance ec2 instance id: " + instanceID)
	}

	region, err := pkg.GetRegion(metaSvc)
	if err != nil {
		log.Fatalln("Could not get the region: " + err.Error())
	} else {
		log.Println("Got the region: " + region)
	}
	autoSvc := autoscaling.New(sess, aws.NewConfig().WithRegion(region))
	s3Svc := s3.New(sess, aws.NewConfig().WithRegion(region))
	groupName, err := pkg.GetAutoscalingGroupName(autoSvc, instanceID)
	if err != nil {
		log.Fatalln("Could not get the autoscaling group name: " + err.Error())
	} else {
		log.Println("Got the autoscaling group name: " + groupName)
	}
	group, err := pkg.GetAutoscalingGroup(autoSvc, groupName)
	if err != nil {
		log.Fatalln("Could not get the autoscaling group : " + err.Error())
	}

	for {
		kubeStatus := pkg.KubeUp(apiDNS, apiPort)

		if !kubeStatus {
			err = pkg.WaitTillCapacityReached(group, 600)
			if err != nil {
				log.Fatalln("Capacity of autoscaling group was not reached : " + err.Error())
			}
			if instanceID == pkg.GetAutoscalingInstances(group)[0] {
				initController(s3Svc, bucket)
			}
		}

		caExists, err := pkg.ExistsOnS3(s3Svc, bucket, &caKeys)
		if err != nil {
			log.Fatalln("Could not fetch pki status from S3: " + err.Error())
		}
		if kubeStatus && caExists {
			joinController(s3Svc, apiDNS, apiPort, bucket)
			return
		}
		time.Sleep(time.Second * 1)
	}
}

var controllerCmd = &cobra.Command{
	Use:   "controller",
	Short: "Deploy controller",
	Long:  `Deploys a HA controller on AWS.`,
	Run: func(cmd *cobra.Command, args []string) {
		log.Println("Start provisioning of the controller")
		deployController(kubeAddress, kubePort, bucket)
	},
}

func init() {
	RootCmd.AddCommand(controllerCmd)
}
