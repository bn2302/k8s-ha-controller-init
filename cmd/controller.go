package cmd

import (
	"encoding/base64"
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
	log.Println("Start kubeadm init")
	kerr := exec.Command(
		"kubeadm",
		"init",
		"--config",
		clusterConfig["kubeadm-cfg-init.yaml"],
	).Run()
	if kerr != nil {
		log.Fatalln("Couldn't run kubeadm: " + kerr.Error())
	}
	log.Println("Deploy weavnet")

	out, kverr := exec.Command(
		"kubectl",
		"--kubeconfig",
		"/etc/kubernetes/admin.conf",
		"version",
	).Output()
	if kverr != nil {
		log.Fatalln("Couldn't get kubernetes version: " + kverr.Error())
	}
	log.Println(out)

	bout := base64.StdEncoding.EncodeToString(out)

	log.Println("https://cloud.weave.works/k8s/net?k8s-version=" + bout)
	werr := exec.Command(
		"kubectl",
		"apply",
		"--kubeconfig",
		"/etc/kubernetes/admin.conf",
		"-f",
		"https://cloud.weave.works/k8s/net?k8s-version="+bout,
	).Run()
	if werr != nil {
		log.Fatalln("Couldn't deploy weavenet: " + kerr.Error())
	}
	clusterInfoData, cerr := exec.Command(
		"kubectl",
		"--kubeconfig",
		"/etc/kubernetes/admin.conf",
		"get",
		"cm",
		"-n",
		"kube-public",
		"cluster-info",
		"-o",
		"jsonpath={.data.kubeconfig}",
	).Output()
	if cerr != nil {
		log.Fatalln("Couldn't get cluster info: " + cerr.Error())
	}
	ferr := ioutil.WriteFile(clusterConfig["cluster-info.yaml"], clusterInfoData, 0644)
	if ferr != nil {
		log.Fatalln("Couldn't write cluster info: " + ferr.Error())
	}
}

func joinController(svc s3iface.S3API, apiDNS string, apiPort int, bucket string) {
	merr := os.MkdirAll("/etc/kubernetes/pki/etcd", 0777)
	if merr != nil {
		log.Fatalln("Could not create directory : " + merr.Error())
	}
	dperr := pkg.DownloadMapFromS3(svc, bucket, &caKeys)
	if dperr != nil {
		log.Fatalln("Download create directory : " + dperr.Error())
	}
	cerr := pkg.DownloadFromS3(svc, bucket, "cluster-info.yaml", clusterConfig["cluster-info.yaml"])
	if cerr != nil {
		log.Fatalln("Download cluster info failed : " + cerr.Error())
	}
	kerr := pkg.DownloadFromS3(svc, bucket, "kubeadm-cfg-join.yaml", clusterConfig["kubeadm-cfg-join.yaml"])
	if kerr != nil {
		log.Fatalln("Download kubeconfig failed : " + kerr.Error())
	}
	err := exec.Command(
		"kubeadm",
		"join",
		apiDNS+":"+strconv.Itoa(apiPort),
		"--config",
		clusterConfig["kubeadm-cfg-join.yaml"],
		"--experimental-control-plane",
	).Run()
	if err != nil {
		log.Fatalln("Kubeadm join failed: " + err.Error())
	}
}

func initController(svc s3iface.S3API, bucket string) {
	if val, serr := pkg.ExistsOnS3(svc, bucket, &caKeys); serr != nil {
		log.Fatalln("Could not check if package exists: " + serr.Error())
	} else if val {
		log.Println("Pki does exist download it")
		merr := os.MkdirAll("/etc/kubernetes/pki/etcd", 0777)
		if merr != nil {
			log.Fatalln("Could not create directory : " + merr.Error())
		}
		dperr := pkg.DownloadMapFromS3(svc, bucket, &caKeys)
		if dperr != nil {
			log.Fatalln("Download create directory : " + dperr.Error())
		}
	} else {
		log.Println("Pki doesn't  exist create ite during kube setup")
	}

	dcerr := pkg.DownloadFromS3(svc, bucket, "kubeadm-cfg-init.yaml", clusterConfig["kubeadm-cfg-init.yaml"])
	if dcerr != nil {
		log.Fatalln("Could not download from S3 : " + dcerr.Error())
	} else {
		log.Println("Downloaded kubeadm.cfg")
	}

	createController()
	if val, serr := pkg.ExistsOnS3(svc, bucket, &caKeys); serr != nil {
		log.Fatalln("Could not check if package exists: " + serr.Error())
	} else if !val {
		uperr := pkg.UploadMapToS3(svc, bucket, &caKeys)
		if uperr != nil {
			log.Fatalln("Could not upload pki to S3 : " + uperr.Error())
		}
	}
	ucerr := pkg.UploadToS3(svc, bucket, "cluster-info.yaml", clusterConfig["cluster-info.yaml"])
	if ucerr != nil {
		log.Fatalln("Could not upload clouser info to S3 : " + ucerr.Error())

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

	log.Println("Start deployment loop")

	for {
		kubeStatus := pkg.KubeUp(apiDNS, apiPort)
		if kubeStatus {
			log.Println("k8s is running")
		} else {
			log.Println("k8s isn't running")
		}

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
