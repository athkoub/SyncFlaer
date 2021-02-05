package sf

import (
	"fmt"
	"github.com/cloudflare/cloudflare-go"
	log "github.com/sirupsen/logrus"
	"strconv"
	"strings"
)

var cf *cloudflare.API
var zoneID string

// SetupCloudflareClient creates a global var with a Cloudflare client instance
func SetupCloudflareClient() {
	var err error
	cf, err = cloudflare.New(config.Cloudflare.APIKey, config.Cloudflare.Email)
	if err != nil {
		log.Fatalf("Unable to setup Cloudflare client: %s", err)
	}
}

// GetCloudflareZoneID creates a global var containing the zone id
func GetCloudflareZoneID() {
	var err error
	zoneID, err = cf.ZoneIDByName(config.Cloudflare.ZoneName)
	if err != nil {
		log.Fatalf("Unable to get Cloudflare zone id: %s", err)
	}
	log.Debugf("Using Cloudflare DNS zone with id %s", zoneID)
}

// GetCloudflareDNSRecords gathers all DNS records in a given zone
func GetCloudflareDNSRecords() []cloudflare.DNSRecord {
	dnsRecords, err := cf.DNSRecords(zoneID, cloudflare.DNSRecord{})
	if err != nil {
		log.Fatalf("Unable to get Cloudflare DNS records: %s", err)
	}

	var cloudflareDNSRecords []cloudflare.DNSRecord
	var cloudflareDNSRecordNames []string

	for _, dnsRecord := range dnsRecords {
		if dnsRecord.Type != "CNAME" && dnsRecord.Type != "A" {
			continue
		}
		cloudflareDNSRecords = append(cloudflareDNSRecords, dnsRecord)
		cloudflareDNSRecordNames = append(cloudflareDNSRecordNames, dnsRecord.Name)
	}
	log.Debugf("Found Cloudflare DNS records: %s", strings.Join(cloudflareDNSRecordNames, ", "))

	return cloudflareDNSRecords
}

// GetDeleteGraceRecords gathers all delete grace DNS records in a given zone
func GetDeleteGraceRecords() []cloudflare.DNSRecord {
	dnsRecords, err := cf.DNSRecords(zoneID, cloudflare.DNSRecord{
		Type: "TXT",
	})
	if err != nil {
		log.Fatalf("Unable to get delete grace DNS records: %s", err)
	}

	var deleteGraceRecords []cloudflare.DNSRecord
	var deleteGraceRecordNames []string

	for _, dnsRecord := range dnsRecords {
		if !strings.Contains(dnsRecord.Name, "_syncflaer._deletegrace.") {
			continue
		}
		deleteGraceRecordNames = append(deleteGraceRecordNames, dnsRecord.Name)
		deleteGraceRecords = append(deleteGraceRecords, dnsRecord)
	}
	log.Debugf("Found delete grace DNS records: %s", strings.Join(deleteGraceRecordNames, " ,"))
	return deleteGraceRecords
}

// CreateCloudflareDNSRecord is a wrapper function to create a DNS record
func CreateCloudflareDNSRecord(record cloudflare.DNSRecord) {
	_, err := cf.CreateDNSRecord(zoneID, record)
	if err != nil {
		errMsg := fmt.Sprintf("Unable to create DNS record %s: %s", record.Name, err)
		addSlackMessage(errMsg, "danger")
		log.Error(errMsg)
		return
	}

	infoMsg := fmt.Sprintf("Created: name: %s, type: %s, content: %s, proxied: %t, ttl: %d", record.Name, record.Type, record.Content, record.Proxied, record.TTL)
	addSlackMessage(infoMsg, "good")
	log.Info(infoMsg)
}

// DeleteCloudflareDNSRecord is a wrapper function to delete a DNS record
func DeleteCloudflareDNSRecord(record cloudflare.DNSRecord) {
	err := cf.DeleteDNSRecord(zoneID, record.ID)
	if err != nil {
		errMsg := fmt.Sprintf("Unable to delete DNS record %s: %s", record.Name, err)
		addSlackMessage(errMsg, "danger")
		log.Error(errMsg)
		return
	}

	infoMsg := fmt.Sprintf("Deleted: %s", record.Name)
	addSlackMessage(infoMsg, "good")
	log.Info(infoMsg)
}

// UpdateCloudflareDNSRecords updates the public IP and additionalRecords
func UpdateCloudflareDNSRecords(cloudflareDNSRecords []cloudflare.DNSRecord, userRecords []cloudflare.DNSRecord) {
	for _, dnsRecord := range cloudflareDNSRecords {
		for _, userRecord := range userRecords {
			if dnsRecord.Name != userRecord.Name {
				continue
			}
			if dnsRecord.Proxied == userRecord.Proxied && dnsRecord.TTL == userRecord.TTL && dnsRecord.Content == userRecord.Content {
				continue
			}
			updatedDNSRecord := cloudflare.DNSRecord{
				Type:    userRecord.Type,
				Content: userRecord.Content,
				Proxied: userRecord.Proxied,
				TTL:     userRecord.TTL,
			}
			err := cf.UpdateDNSRecord(zoneID, dnsRecord.ID, updatedDNSRecord)
			if err != nil {
				errMsg := fmt.Sprintf("Unable to update DNS record %s: %s", dnsRecord.Name, err)
				addSlackMessage(errMsg, "danger")
				log.Error(errMsg)
				continue
			}

			infoMsg := fmt.Sprintf("Updated: name: %s, type: %s, content: %s, proxied: %t, ttl: %d", dnsRecord.Name, updatedDNSRecord.Type, updatedDNSRecord.Content, updatedDNSRecord.Proxied, updatedDNSRecord.TTL)
			addSlackMessage(infoMsg, "good")
			log.Info(infoMsg)
		}
	}
}

// GetMissingDNSRecords compares Cloudflare DNS records with Traefik rules and additionalRecords
func GetMissingDNSRecords(cloudflareDNSRecords []cloudflare.DNSRecord, userRecords []cloudflare.DNSRecord) []cloudflare.DNSRecord {
	var missingRecords []cloudflare.DNSRecord

	for _, userRecord := range userRecords {
		recordFound := false
		for _, cloudflareDNSRecord := range cloudflareDNSRecords {
			if userRecord.Name == cloudflareDNSRecord.Name {
				recordFound = true
			}
		}
		if !recordFound {
			missingRecords = append(missingRecords, userRecord)
		}
	}

	return missingRecords
}

// GetOrphanedDNSRecords compares Cloudflare DNS records with Traefik rules and additionalRecords
func GetOrphanedDNSRecords(cloudflareDNSRecords []cloudflare.DNSRecord, userRecords []cloudflare.DNSRecord) []cloudflare.DNSRecord {
	var orphanedRecords []cloudflare.DNSRecord

	for _, cloudflareDNSRecord := range cloudflareDNSRecords {
		recordFound := false
		for _, userRecord := range userRecords {
			if userRecord.Name == cloudflareDNSRecord.Name {
				recordFound = true
			}
		}
		if !recordFound {
			orphanedRecords = append(orphanedRecords, cloudflareDNSRecord)
		}
	}

	return orphanedRecords
}

func GetDeleteGraceRecord(orphanedRecordName string, deleteGraceRecords []cloudflare.DNSRecord) cloudflare.DNSRecord {
	var deleteGraceRecordFound cloudflare.DNSRecord
	for _, deleteGraceRecord := range deleteGraceRecords {
		if !strings.Contains(deleteGraceRecord.Name, orphanedRecordName) {
			continue
		}
		deleteGraceRecordFound = deleteGraceRecord
	}
	return deleteGraceRecordFound
}

func CreateDeleteGraceRecord(orphanedRecordName string) {
	deleteGraceRecord := cloudflare.DNSRecord{
		Type:    "TXT",
		Name:    fmt.Sprintf("_syncflaer._deletegrace.%s", orphanedRecordName),
		Content: strconv.Itoa(config.Cloudflare.DeleteGrace),
	}
	_, err := cf.CreateDNSRecord(zoneID, deleteGraceRecord)
	if err != nil {
		log.Errorf("Unable to create delete grace DNS record %s: %s", deleteGraceRecord.Name, err)
		return
	}

	log.Infof("Waiting %s more runs until DNS record %s gets deleted", deleteGraceRecord.Content, orphanedRecordName)
}

func UpdateDeleteGraceRecord(deleteGraceRecord cloudflare.DNSRecord, orphanedRecordName string) {
	newDeleteGrace, _ := strconv.Atoi(deleteGraceRecord.Content)
	newDeleteGrace -= 1
	deleteGraceRecord.Content = strconv.Itoa(newDeleteGrace)
	err := cf.UpdateDNSRecord(zoneID, deleteGraceRecord.ID, deleteGraceRecord)
	if err != nil {
		log.Error("Unable to update delete grace DNS record %s: %s", deleteGraceRecord.Name, err)
		return
	}

	log.Infof("Waiting %s more runs until DNS record %s gets deleted", deleteGraceRecord.Content, orphanedRecordName)
}
