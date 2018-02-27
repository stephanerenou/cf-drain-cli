package main

import (
	"crypto/tls"
	"log"
	"net/http"
	"time"

	"code.cloudfoundry.org/cf-drain-cli/internal/cloudcontroller"
)

func main() {
	log.Printf("starting space drain...")
	defer log.Printf("space drain closing...")

	cfg := loadConfig()

	httpClient := &http.Client{
		Timeout: 5 * time.Second,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{
				InsecureSkipVerify: cfg.SkipCertVerify,
			},
		},
	}

	tokenFetcher := cloudcontroller.NewUAATokenFetcher(
		cfg.UAAAddr,
		cfg.ClientID,
		cfg.ClientSecret,
		cfg.Username,
		cfg.Password,
		httpClient,
	)

	curler := cloudcontroller.NewHTTPCurlClient(cfg.APIAddr, httpClient, tokenFetcher)

	drainLister := cloudcontroller.NewListDrainsClient(curler)
	drainCreator := cloudcontroller.NewCreateDrainClient(curler)
	drainBinder := cloudcontroller.NewBindDrainClient(curler)
	appLister := cloudcontroller.NewAppListerClient(curler)

	for range time.Tick(time.Minute) {
		drains, err := drainLister.Drains(cfg.SpaceID)
		if err != nil {
			log.Printf("failed to fetch drains: %s", err)
			continue
		}

		drain, ok := hasDrain(cfg.DrainName, drains)
		if !ok {
			log.Printf("creating %s drain...", cfg.DrainName)
			if err := drainCreator.CreateDrain(
				cfg.DrainName,
				cfg.DrainURL,
				cfg.SpaceID,
				cfg.DrainType,
			); err != nil {
				log.Printf("failed to create drain: %s", err)
				continue
			}
			log.Printf("created %s drain", cfg.DrainName)

			// Continue again so that ListDrains can find it and get its guid.
			continue
		}
		apps, err := appLister.ListApps(cfg.SpaceID)
		if err != nil {
			log.Printf("failed to list apps: %s", err)
			continue
		}

		log.Printf("binding apps to drain...")
		for _, appGuid := range apps {
			if containsApp(appGuid, drain.AppGuids) {
				continue
			}

			if err := drainBinder.BindDrain(appGuid, drain.Guid); err != nil {
				log.Printf("failed to bind %s to drain: %s", appGuid, err)
				continue
			}
			drain.AppGuids = append(drain.AppGuids, appGuid)
		}
		log.Printf("done binding apps to drain.")
	}
}

func containsApp(appGuid string, guids []string) bool {
	for _, g := range guids {
		if g == appGuid {
			return true
		}
	}

	return false
}

func hasDrain(name string, drains []cloudcontroller.Drain) (cloudcontroller.Drain, bool) {
	for _, drain := range drains {
		if drain.Name == name {
			return drain, true
		}
	}

	return cloudcontroller.Drain{}, false
}