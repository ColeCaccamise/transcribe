# AWS Transcribe Utility

A simple utility to transcribe audio files (MP3 or MP4) using AWS Transcribe service.

## Prerequisites

- AWS account with access to Transcribe and S3 services
- AWS credentials configured locally
- Go installed on your machine
- ffmpeg installed on your machine (if you want to convert MP4 to MP3)

## Usage

1. Prepare your audio file (MP3 or MP4 format)
2. Run the utility with your file:

```bash
go run main.go /path/to/your_audio_file.mp3
```

## Output

The utility will create a `[filename]_[timestamp].txt` file in the same directory as the audio file, containing the transcribed text.

## AWS Policy

Create an IAM user with the following policy attached:

```json
{
    "Version": "2012-10-17",
    "Statement": [
        {
            "Sid": "VisualEditor0",
            "Effect": "Allow",
            "Action": [
                "transcribe:GetTranscriptionJob",
                "transcribe:StartTranscriptionJob"
            ],
            "Resource": "*"
        },
        {
            "Sid": "Statement1",
            "Effect": "Allow",
            "Action": [
                "s3:PutObject",
                "s3:GetObject"
            ],
            "Resource": [
                "arn:aws:s3:::[your-bucket-name]/*"
            ]
        }
    ]
}
```
