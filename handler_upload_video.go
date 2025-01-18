package main

import (
	"fmt"
	"io"
	"mime"
	"net/http"
	"os"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/bootdotdev/learn-file-storage-s3-golang-starter/internal/auth"
	"github.com/google/uuid"
)

func (cfg *apiConfig) handlerUploadVideo(w http.ResponseWriter, r *http.Request) {
	_ = http.MaxBytesReader(w, r.Body, 1<<30)
	videoIDString := r.PathValue("videoID")
	videoID, err := uuid.Parse(videoIDString)
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Invalid video ID", err)
		return
	}

	token, err := auth.GetBearerToken(r.Header)
	if err != nil {
		respondWithError(w, http.StatusUnauthorized, "Couldn't find JWT", err)
		return
	}
	userID, err := auth.ValidateJWT(token, cfg.jwtSecret)
	if err != nil {
		respondWithError(w, http.StatusUnauthorized, "Couldn't validate JWT", err)
		return
	}

	video, err := cfg.db.GetVideo(videoID)
	if err != nil {
		respondWithError(w, http.StatusNotFound, "Couldn't get video", err)
		return
	}
	if video.UserID != userID {
		respondWithError(w, http.StatusForbidden, "You can't delete this video", err)
		return
	}

	videoFile, videoHeader, err := r.FormFile("video")
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "invalid file", err)
		return
	}
	defer videoFile.Close()

	mediaType, _, err := mime.ParseMediaType(videoHeader.Header.Get("content-type"))

	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Invalid content type", err)
		return
	}
	if mediaType != "video/mp4" {
		respondWithError(w, http.StatusBadRequest, "File must be an MP4", nil)
		return
	}

	f, err := os.CreateTemp("", "tubely-upload.mp4")
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "coulnl't upload file", nil)
		return
	}
	defer os.Remove(f.Name())
	defer f.Close()

	_, err = io.Copy(f, videoFile)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Failed to copy file", err)
		return
	}

	_, err = f.Seek(0, io.SeekStart)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Failed to reset file", err)
		return
	}

	key := fmt.Sprintf("%x.mp4", uuid.New())

	_, err = cfg.s3Client.PutObject(r.Context(), &s3.PutObjectInput{
		Bucket:      aws.String(cfg.s3Bucket),
		Key:         aws.String(key),
		Body:        f,
		ContentType: aws.String(mediaType),
	})
	if err != nil {
		fmt.Printf("S3 error details: %v\n", err)
		respondWithError(w, http.StatusInternalServerError, "Failed to upload to S3", err)
		return
	}

	videoURL := fmt.Sprintf("https://%s.s3.%s.amazonaws.com/%s",
		cfg.s3Bucket,
		cfg.s3Region,
		key)

	video.VideoURL = &videoURL
	err = cfg.db.UpdateVideo(video)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Couldn't update video URL", err)
		return
	}

	respondWithJSON(w, http.StatusOK, video)
}
