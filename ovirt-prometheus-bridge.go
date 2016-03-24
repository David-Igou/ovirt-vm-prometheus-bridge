package main

import (
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"flag"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"strings"
	"time"
)

type Targets struct {
	Targets []string          `json:"targets"`
	Labels  map[string]string `json:"labels"`
}

type Hosts struct {
	Host []Host
}

type Host struct {
	Address string
	Cluster Cluster
}

type Cluster struct {
	Id string
}

type Config struct {
	Target         string
	URL            string
	User           string
	Password       string
	NoVerify       bool
	EngineCA       string
	UpdateInterval int
}

func main() {
	target := flag.String("output", "engine-hosts.json", "target for the configuration file")
	engineURL := flag.String("engine-url", "https://localhost:8443", "Engine URL")
	engineUser := flag.String("engine-user", "admin@internal", "Engine user")
	enginePassword := flag.String("engine-password", "", "Engine password. Consider using ENGINE_PASSWORD environment variable to set this")
	noVerify := flag.Bool("no-verify", false, "Don't verify the engine certificate")
	engineCa := flag.String("engine-ca", "/etc/pki/vdsm/certs/cacert.pem", "Path to engine ca certificate")
	updateInterval := flag.Int("update-interval", 60, "Update intervall for host discovery in seconds")
	flag.Parse()
	if *enginePassword == "" {
		*enginePassword = os.Getenv("ENGINE_PASSWORD")
	}
	config := Config{Target: *target,
		URL:            *engineURL,
		User:           *engineUser,
		Password:       *enginePassword,
		NoVerify:       *noVerify,
		EngineCA:       *engineCa,
		UpdateInterval: *updateInterval,
	}

	if !strings.HasPrefix(config.URL, "https") {
		log.Fatal("Only URLs starting with 'https' are supported")
	}
	if config.Password == "" {
		log.Fatal("No engine password supplied")
	}

	tlsConfig := &tls.Config{
		InsecureSkipVerify: config.NoVerify,
	}
	if !config.NoVerify {
		roots := x509.NewCertPool()
		ok := roots.AppendCertsFromPEM(readFile(config.EngineCA))
		if !ok {
			log.Panic("Could not load root CA certificate")
		}

		tlsConfig.RootCAs = roots
	}
	tlsConfig.BuildNameToCertificate()
	transport := &http.Transport{TLSClientConfig: tlsConfig}
	client := &http.Client{Transport: transport}
	for {
		Discover(client, &config)
		time.Sleep(time.Duration(config.UpdateInterval) * time.Second)
	}
}

func Discover(client *http.Client, config *Config) {
	req, err := http.NewRequest("GET", config.URL+"/ovirt-engine/api/hosts", nil)
	check(err)
	req.Header.Add("Accept", "application/json")
	req.SetBasicAuth(config.User, config.Password)
	res, err := client.Do(req)
	if err != nil {
		log.Print(err)
		return
	}
	hosts, err := ioutil.ReadAll(res.Body)
	res.Body.Close()
	if err != nil {
		log.Print(err)
		return
	}
	writeTargets(config.Target, MapToTarget(ParseJson(hosts)))
}

func ParseJson(data []byte) *Hosts {
	hosts := new(Hosts)
	err := json.Unmarshal(data, hosts)
	check(err)
	return hosts
}

func MapToTarget(hosts *Hosts) []*Targets {
	targetMap := make(map[string]*Targets)
	var targets []*Targets
	for _, host := range hosts.Host {
		if value, ok := targetMap[host.Cluster.Id]; ok {
			value.Targets = append(value.Targets, host.Address)
		} else {
			targetMap[host.Cluster.Id] = &Targets{
				Labels:  map[string]string{"cluster": host.Cluster.Id},
				Targets: []string{host.Address}}
			targets = append(targets, targetMap[host.Cluster.Id])
		}
	}
	return targets
}

func writeTargets(fileName string, targets []*Targets) {
	data, _ := json.MarshalIndent(targets, "", "  ")
	data = append(data, '\n')
	err := ioutil.WriteFile(fileName, data, 0644)
	check(err)
}

func check(e error) {
	if e != nil {
		log.Fatal(e)
	}
}

func readFile(fileName string) []byte {
	bytes, err := ioutil.ReadFile(fileName)
	check(err)
	return bytes
}
