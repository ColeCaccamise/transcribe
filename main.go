package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/transcribe"
	"github.com/aws/aws-sdk-go-v2/service/transcribe/types"
	"github.com/joho/godotenv"
)

// asrOutput represents the structure of our JSON file
type asrOutput struct {
	Results struct {
		Transcripts []struct {
			Transcript string `json:"transcript"`
		} `json:"transcripts"`
	} `json:"results"`
}

// printProgress prints a loading indicator with the given message
func printProgress(message string, progress int) {
	// clear line
	fmt.Print("\r\033[K")
	
	// create progress bar
	barWidth := 50
	filled := progress * barWidth / 100
	bar := strings.Repeat("=", filled) + strings.Repeat(" ", barWidth-filled)
	
	// print progress bar and message
	fmt.Printf("\r%s [%s] %d%%", message, bar, progress)
}

func main() {
	// load .env file
	if err := godotenv.Load(); err != nil {
		fmt.Printf("error loading .env file: %v\n", err)
		os.Exit(1)
	}

	if len(os.Args) != 2 {
		fmt.Println("usage: transcribe <input_file>")
		os.Exit(1)
	}

	inputFile := os.Args[1]
	if _, err := os.Stat(inputFile); os.IsNotExist(err) {
		fmt.Printf("error: file %s does not exist\n", inputFile)
		os.Exit(1)
	}

	// get input file directory
	inputDir := filepath.Dir(inputFile)
	
	// generate unique output filenames
	baseName := filepath.Base(inputFile)
	ext := filepath.Ext(baseName)
	nameWithoutExt := strings.TrimSuffix(baseName, ext)
	timestamp := time.Now().Format("20060102_150405")
	
	// create output filenames in same directory as input
	audioFile := filepath.Join(inputDir, fmt.Sprintf("%s_%s.mp3", nameWithoutExt, timestamp))
	transcriptFile := filepath.Join(inputDir, fmt.Sprintf("%s_%s.txt", nameWithoutExt, timestamp))
	jsonFile := filepath.Join(inputDir, fmt.Sprintf("%s_%s.json", nameWithoutExt, timestamp))

	// convert video to audio if needed
	if strings.ToLower(ext) == ".mp4" {
		fmt.Printf("converting %s to audio...\n", inputFile)
		cmd := exec.Command("ffmpeg", "-i", inputFile, "-vn", "-acodec", "libmp3lame", audioFile)
		if err := cmd.Run(); err != nil {
			fmt.Printf("error converting video to audio: %v\n", err)
			os.Exit(1)
		}
		inputFile = audioFile
	}

	// initialize aws config with credentials from env
	cfg, err := config.LoadDefaultConfig(context.TODO(),
		config.WithRegion(os.Getenv("AWS_REGION")),
		config.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(
			os.Getenv("AWS_ACCESS_KEY"),
			os.Getenv("AWS_SECRET_ACCESS_KEY"),
			"",
		)),
	)
	if err != nil {
		fmt.Printf("error loading aws config: %v\n", err)
		os.Exit(1)
	}

	// create s3 client
	s3Client := s3.NewFromConfig(cfg)
	bucketName := "vault" // use just the bucket name without arn prefix

	// upload audio file to s3
	fmt.Printf("uploading %s to s3...\n", inputFile)
	file, err := os.Open(inputFile)
	if err != nil {
		fmt.Printf("error opening audio file: %v\n", err)
		os.Exit(1)
	}
	defer file.Close()

	_, err = s3Client.PutObject(context.TODO(), &s3.PutObjectInput{
		Bucket: aws.String(bucketName),
		Key:    aws.String(inputFile),
		Body:   file,
	})
	if err != nil {
		fmt.Printf("error uploading to s3: %v\n", err)
		os.Exit(1)
	}

	// create transcribe client
	transcribeClient := transcribe.NewFromConfig(cfg)

	// start transcription job
	jobName := fmt.Sprintf("%s_%s", nameWithoutExt, timestamp)
	input := &transcribe.StartTranscriptionJobInput{
		TranscriptionJobName: aws.String(jobName),
		Media: &types.Media{
			MediaFileUri: aws.String(fmt.Sprintf("s3://%s/%s", bucketName, inputFile)),
		},
		LanguageCode: types.LanguageCodeEnUs,
		OutputBucketName: aws.String(bucketName),
		OutputKey: aws.String(jsonFile),
	}

	fmt.Printf("starting transcription job %s...\n", jobName)
	_, err = transcribeClient.StartTranscriptionJob(context.TODO(), input)
	if err != nil {
		fmt.Printf("error starting transcription job: %v\n", err)
		os.Exit(1)
	}

	// wait for job completion with progress indicator
	fmt.Println("waiting for transcription to complete...")
	startTime := time.Now()
	for {
		output, err := transcribeClient.GetTranscriptionJob(context.TODO(), &transcribe.GetTranscriptionJobInput{
			TranscriptionJobName: aws.String(jobName),
		})
		if err != nil {
			fmt.Printf("\nerror checking job status: %v\n", err)
			os.Exit(1)
		}

		// calculate progress based on elapsed time (rough estimate)
		elapsed := time.Since(startTime)
		progress := int(elapsed.Seconds() / 2) // assume 2 seconds per percent
		if progress > 100 {
			progress = 100
		}

		// show progress
		printProgress("Transcribing...", progress)

		if output.TranscriptionJob.TranscriptionJobStatus == types.TranscriptionJobStatusCompleted {
			fmt.Println("\nTranscription completed!")
			break
		} else if output.TranscriptionJob.TranscriptionJobStatus == types.TranscriptionJobStatusFailed {
			fmt.Println("\nTranscription job failed")
			os.Exit(1)
		}

		time.Sleep(1 * time.Second)
	}

	// download and process the transcript
	fmt.Println("processing transcript...")
	
	// download the json file from s3
	result, err := s3Client.GetObject(context.TODO(), &s3.GetObjectInput{
		Bucket: aws.String(bucketName),
		Key:    aws.String(jsonFile),
	})
	if err != nil {
		fmt.Printf("error downloading transcript: %v\n", err)
		os.Exit(1)
	}

	// read the json data
	jsonData, err := io.ReadAll(result.Body)
	if err != nil {
		fmt.Printf("error reading transcript data: %v\n", err)
		os.Exit(1)
	}

	// parse the json data
	var output asrOutput
	if err := json.Unmarshal(jsonData, &output); err != nil {
		fmt.Printf("error parsing transcript: %v\n", err)
		os.Exit(1)
	}

	// check if we have any transcripts
	if len(output.Results.Transcripts) == 0 {
		fmt.Println("no transcripts found in the file")
		os.Exit(1)
	}

	// write the transcript to a text file
	err = os.WriteFile(transcriptFile, []byte(output.Results.Transcripts[0].Transcript), 0644)
	if err != nil {
		fmt.Printf("error writing transcript file: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("transcription completed successfully. output saved to %s\n", transcriptFile)
}
