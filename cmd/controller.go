package cmd

import (
	"bytes"
	"encoding/base64"
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/ec2metadata"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/autoscaling"
	"github.com/aws/aws-sdk-go/service/s3"
	"github.com/aws/aws-sdk-go/service/s3/s3iface"
	"github.com/bn2302/k8s-ha-controller-init/pkg"
	"github.com/spf13/cobra"
	"io"
	"io/ioutil"
	"log"
	"os"
	"os/exec"
	"strconv"
	"time"
)

func getKubeVersion() ([]byte, error) {
	retry := 0
	for {
		out, err := exec.Command(
			"kubectl",
			"--kubeconfig",
			"/etc/kubernetes/admin.conf",
			"version",
		).Output()
		if retry > 10 && err != nil {
			return nil, err
		} else if err == nil {
			return out, nil
		}
		retry++
		time.Sleep(time.Second * 1)
	}
}

func createController() {
	log.Println("---- kubeadm init ----")
	kubeadmCmd := exec.Command(
		"kubeadm",
		"init",
		"--config",
		clusterConfig["kubeadm-cfg-init.yaml"],
	)
	kubeadmCmd.Stdout = os.Stdout
	if err := kubeadmCmd.Run(); err != nil {
		log.Panic("Couldn't run kubeadm: " + err.Error())
	}

	log.Println("---- Deploy weavnet ----")
	kubeVersionRaw, err := getKubeVersion()
	if err != nil {
		log.Fatalln("Couldn't get kubernetes version: " + err.Error())
	}
	kubeVersion := base64.StdEncoding.EncodeToString(kubeVersionRaw)
	deployWeaveCmd := exec.Command(
		"kubectl",
		"apply",
		"--kubeconfig",
		"/etc/kubernetes/admin.conf",
		"-f",
		"https://cloud.weave.works/k8s/net?k8s-version="+kubeVersion,
	)
	deployWeaveCmd.Stdout = os.Stdout
	if err := deployWeaveCmd.Run(); err != nil {
		log.Panic("Couldn't deploy weavenet: " + err.Error())
	}

	log.Println("---- Write cluster info ----")
	clusterInfoCmd := exec.Command(
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
	)
	var clusterInfoBuffer bytes.Buffer
	mw := io.MultiWriter(os.Stdout, &clusterInfoBuffer)
	clusterInfoCmd.Stdout = mw
	clusterInfoCmd.Stderr = mw
	if err := clusterInfoCmd.Run(); err != nil {
		log.Panic("Couldn't get cluster info: " + err.Error())
	}
	if err := ioutil.WriteFile(clusterConfig["cluster-info.yaml"], clusterInfoBuffer.Bytes(), 0644); err != nil {
		log.Panic("Couldn't write cluster info: " + err.Error())
	}
}

func joinController(svc s3iface.S3API, apiDNS string, apiPort int, bucket string) {
	err := os.MkdirAll("/etc/kubernetes/pki/etcd", 0777)
	if err != nil {
		log.Fatalln("Could not create directory : " + err.Error())
	}
	err = pkg.DownloadMapFromS3(svc, bucket, &caKeys)
	if err != nil {
		log.Fatalln("Download create directory : " + err.Error())
	}
	err = pkg.DownloadFromS3(svc, bucket, "cluster-info.yaml", clusterConfig["cluster-info.yaml"])
	if err != nil {
		log.Fatalln("Download cluster info failed : " + err.Error())
	}
	err = pkg.DownloadFromS3(svc, bucket, "kubeadm-cfg-join.yaml", clusterConfig["kubeadm-cfg-join.yaml"])
	if err != nil {
		log.Fatalln("Download kubeconfig failed : " + err.Error())
	}

	joinCmd := exec.Command(
		"kubeadm",
		"join",
		apiDNS+":"+strconv.Itoa(apiPort),
		"--config",
		clusterConfig["kubeadm-cfg-join.yaml"],
		"--control-plane",
	)
	joinCmd.Stdout = os.Stdout
	if err := joinCmd.Run(); err != nil {
		log.Fatalln("Kubeadm join failed: " + err.Error())
	}
}

func initController(svc s3iface.S3API, bucket string) {
	if val, err := pkg.ExistsOnS3(svc, bucket, &caKeys); err != nil {
		log.Fatalln("Could not check if package exists: " + err.Error())
	} else if val {
		log.Println("Pki does exist download it")
		err := os.MkdirAll("/etc/kubernetes/pki/etcd", 0777)
		if err != nil {
			log.Fatalln("Could not create directory : " + err.Error())
		}
		err = pkg.DownloadMapFromS3(svc, bucket, &caKeys)
		if err != nil {
			log.Fatalln("Download create directory : " + err.Error())
		}
	} else {
		log.Println("Pki doesn't  exist create ite during kube setup")
	}

	err := pkg.DownloadFromS3(svc, bucket, "kubeadm-cfg-init.yaml", clusterConfig["kubeadm-cfg-init.yaml"])
	if err != nil {
		log.Fatalln("Could not download from S3 : " + err.Error())
	} else {
		log.Println("Downloaded kubeadm.cfg")
	}
	createController()
	if val, err := pkg.ExistsOnS3(svc, bucket, &caKeys); err != nil {
		log.Fatalln("Could not check if package exists: " + err.Error())
	} else if !val {
		err = pkg.UploadMapToS3(svc, bucket, &caKeys)
		if err != nil {
			log.Fatalln("Could not upload pki to S3 : " + err.Error())
		}
	}
	err = pkg.UploadToS3(svc, bucket, "cluster-info.yaml", clusterConfig["cluster-info.yaml"])
	if err != nil {
		log.Fatalln("Could not upload cluster info to S3 : " + err.Error())
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

	if pkg.KubeUp("127.0.0.1", apiPort) {
		log.Println("Kubernetes is already running")
		return
	}

	log.Println("Wait till DNS resolves")
	pkg.DNSResolves(apiDNS)

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
				return
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
