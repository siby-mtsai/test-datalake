// Command compact is the mtsai-datalake weekly hygiene job (brief Phase 3 step 2): runs Athena's
// Iceberg VACUUM statement on each Iceberg table - Athena's equivalent of what Spark exposes as
// two separate procedures, expire_snapshots and remove_orphan_files - and publishes the cost
// dashboard's S3-storage-by-prefix metric, since CloudWatch's native S3 metrics are bucket-level
// only.
package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/athena"
	"github.com/aws/aws-sdk-go-v2/service/cloudwatch"
	cwtypes "github.com/aws/aws-sdk-go-v2/service/cloudwatch/types"
	"github.com/aws/aws-sdk-go-v2/service/s3"

	"mtsai-datalake-export/internal/lake"
)

const metricNamespace = "MTSAiDataLake/Storage"

// athena-results/export/ (not the whole athena-results/ zone) - the task role's ListBucket grant
// only covers its own subfolder (AthenaResultsList in terraform/modules/export-task/main.tf),
// deliberately not the whole zone (that would leak key enumeration across the other consumer
// workgroups' own results, the exact class of bug already found and fixed once in Phase 1).
// Found this the hard way: listing the bare "athena-results/" prefix under the real task role
// failed with AccessDenied, while raw/ and curated/ (both broadly granted) succeeded.
var storagePrefixes = []string{"raw/", "curated/", "athena-results/export/", "export-manifests/"}

// vacuumTarget is one (database, table) pair to run weekly hygiene on. Hardcoded for this v1
// slice - matches how cmd/export/cmd/curate also hardcode trip_events rather than iterating a
// config that isn't fully wired to drive table lists yet.
type vacuumTarget struct {
	database string
	table    string
}

func main() {
	if err := run(); err != nil {
		log.Fatalf("compact failed: %v", err)
	}
}

type config struct {
	Bucket          string
	RawDatabase     string
	CuratedDatabase string
	Region          string
	WorkGroup       string
}

func loadConfig() (*config, error) {
	cfg := &config{
		Bucket:          os.Getenv("MTSAI_DATALAKE_BUCKET"),
		RawDatabase:     os.Getenv("MTSAI_DATALAKE_RAW_DATABASE"),
		CuratedDatabase: os.Getenv("MTSAI_DATALAKE_CURATED_DATABASE"),
		Region:          os.Getenv("AWS_REGION"),
		WorkGroup:       os.Getenv("MTSAI_DATALAKE_WORKGROUP"),
	}
	if cfg.Bucket == "" || cfg.RawDatabase == "" || cfg.CuratedDatabase == "" {
		return nil, fmt.Errorf("MTSAI_DATALAKE_BUCKET, MTSAI_DATALAKE_RAW_DATABASE, and MTSAI_DATALAKE_CURATED_DATABASE are all required")
	}
	if cfg.Region == "" {
		cfg.Region = "ap-south-1"
	}
	return cfg, nil
}

func run() error {
	ctx := context.Background()
	start := time.Now()

	cfg, err := loadConfig()
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}

	awsCfg, err := awsconfig.LoadDefaultConfig(ctx, awsconfig.WithRegion(cfg.Region))
	if err != nil {
		return fmt.Errorf("loading AWS config: %w", err)
	}
	s3Client := s3.NewFromConfig(awsCfg)
	athenaClient := athena.NewFromConfig(awsCfg)
	cwClient := cloudwatch.NewFromConfig(awsCfg)

	targets := []vacuumTarget{
		{database: cfg.RawDatabase, table: "trip_events"},
		{database: cfg.CuratedDatabase, table: "trip_events_curated"},
	}

	for _, t := range targets {
		runner := &lake.AthenaRunner{
			Client:         athenaClient,
			Database:       t.database,
			OutputLocation: fmt.Sprintf("s3://%s/athena-results/export/", cfg.Bucket),
			WorkGroup:      cfg.WorkGroup,
		}
		log.Printf("vacuuming %s.%s", t.database, t.table)
		if _, err := runner.Run(ctx, fmt.Sprintf("VACUUM %s", t.table)); err != nil {
			return fmt.Errorf("vacuum %s.%s: %w", t.database, t.table, err)
		}
		log.Printf("vacuumed %s.%s", t.database, t.table)
	}

	if err := publishStorageMetrics(ctx, s3Client, cwClient, cfg.Bucket); err != nil {
		return fmt.Errorf("publishing storage metrics: %w", err)
	}

	log.Printf("compact run succeeded: %d table(s) vacuumed, duration=%.2fs", len(targets), time.Since(start).Seconds())
	return nil
}

func publishStorageMetrics(ctx context.Context, s3Client *s3.Client, cwClient *cloudwatch.Client, bucket string) error {
	var data []cwtypes.MetricDatum
	for _, prefix := range storagePrefixes {
		size, err := lake.SumObjectSizes(ctx, s3Client, bucket, prefix)
		if err != nil {
			return fmt.Errorf("summing size for %s: %w", prefix, err)
		}
		log.Printf("s3://%s/%s: %d byte(s)", bucket, prefix, size)
		data = append(data, cwtypes.MetricDatum{
			MetricName: aws.String("PrefixBytes"),
			Value:      aws.Float64(float64(size)),
			Unit:       cwtypes.StandardUnitBytes,
			Dimensions: []cwtypes.Dimension{
				{Name: aws.String("Prefix"), Value: aws.String(strings.TrimSuffix(prefix, "/"))},
			},
		})
	}
	_, err := cwClient.PutMetricData(ctx, &cloudwatch.PutMetricDataInput{
		Namespace:  aws.String(metricNamespace),
		MetricData: data,
	})
	return err
}
