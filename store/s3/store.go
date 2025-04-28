package s3

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"path"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/smithy-go"
	"github.com/mashiike/a2a"
)

type Client interface {
	GetObject(context.Context, *s3.GetObjectInput, ...func(*s3.Options)) (*s3.GetObjectOutput, error)
	PutObject(context.Context, *s3.PutObjectInput, ...func(*s3.Options)) (*s3.PutObjectOutput, error)
}

var _ Client = (*s3.Client)(nil)

type Store struct {
	opts       Options
	bucketName string
	client     Client
}

var _ a2a.Store = (*Store)(nil)

type Options struct {
	ObjectKeyPrefix string
	Marshaler       func(any) ([]byte, error)
	Unmarshaller    func([]byte, any) error
}

// WithObjectKeyPrefix sets the prefix for the S3 object key. default is "tasks/"
func WithObjectKeyPrefix(prefix string) func(*Options) {
	return func(o *Options) {
		o.ObjectKeyPrefix = prefix
	}
}

// WithMarshaler sets the marshaler for the S3 object. default is json.Marshal
func WithMarshaler(marshaler func(any) ([]byte, error)) func(*Options) {
	return func(o *Options) {
		o.Marshaler = marshaler
	}
}

// WithUnmarshaller sets the unmarshaller for the S3 object. default is json.Unmarshal
func WithUnmarshaller(unmarshaller func([]byte, any) error) func(*Options) {
	return func(o *Options) {
		o.Unmarshaller = unmarshaller
	}
}

func NewFromConfig(cfg aws.Config, bucketName string, optFns ...any) *Store {
	s3OptFns := make([]func(*s3.Options), 0, len(optFns))
	storeOptFns := make([]func(*Options), 0, len(optFns))
	for _, optFn := range optFns {
		switch opt := optFn.(type) {
		case func(*s3.Options):
			s3OptFns = append(s3OptFns, opt)
		case func(*Options):
			storeOptFns = append(storeOptFns, opt)
		default:
			panic("unsupported option type")
		}
	}
	client := s3.NewFromConfig(cfg, s3OptFns...)
	return NewWithClient(client, bucketName, storeOptFns...)
}

func NewWithClient(client Client, bucketName string, optFns ...func(*Options)) *Store {
	s := &Store{
		client:     client,
		bucketName: bucketName,
		opts: Options{
			ObjectKeyPrefix: "tasks/",
			Marshaler:       json.Marshal,
			Unmarshaller:    json.Unmarshal,
		},
	}
	for _, optFn := range optFns {
		optFn(&s.opts)
	}
	return s
}

func (s *Store) taskObjectKey(taskID string) string {
	return strings.TrimPrefix(path.Join(s.opts.ObjectKeyPrefix, taskID, "task.json"), "/")
}

func (s *Store) pushNotificationConfigKey(taskID string) string {
	return strings.TrimPrefix(path.Join(s.opts.ObjectKeyPrefix, taskID, "push-notification.json"), "/")
}

func (s *Store) getTask(ctx context.Context, taskID string) (*a2a.Task, *string, error) {
	resp, err := s.client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(s.bucketName),
		Key:    aws.String(s.taskObjectKey(taskID)),
	})
	if err != nil {
		var apiErr smithy.APIError
		if ok := errors.As(err, &apiErr); ok && apiErr.ErrorCode() == "NoSuchKey" {
			return nil, nil, a2a.ErrTaskNotFound
		}
		return nil, nil, err
	}
	defer resp.Body.Close()
	var task a2a.Task
	bs, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, nil, err
	}

	if err := s.opts.Unmarshaller(bs, &task); err != nil {
		return nil, nil, err
	}
	return &task, resp.ETag, nil
}

func (s *Store) putTask(ctx context.Context, task *a2a.Task, eTag *string) error {
	input := &s3.PutObjectInput{
		Bucket: aws.String(s.bucketName),
		Key:    aws.String(s.taskObjectKey(task.ID)),
	}
	if eTag == nil {
		input.IfNoneMatch = aws.String("*")
	} else {
		input.IfMatch = eTag
	}
	bs, err := s.opts.Marshaler(task)
	if err != nil {
		return err
	}
	input.Body = bytes.NewReader(bs)
	_, err = s.client.PutObject(ctx, input)
	return err
}

func (s *Store) UpsertTask(ctx context.Context, params a2a.TaskSendParams) (*a2a.Task, error) {
	cur, eTag, err := s.getTask(ctx, params.ID)
	if err != nil {
		if !errors.Is(err, a2a.ErrTaskNotFound) {
			return nil, err
		}
	}
	updated := a2a.MutateTask(cur, params)
	if err := s.putTask(ctx, updated, eTag); err != nil {
		return nil, err
	}
	return a2a.TruncateHistory(updated, params.HistoryLength), nil
}

func (s *Store) GetTask(ctx context.Context, taskID string, historyLength *int) (*a2a.Task, error) {
	task, _, err := s.getTask(ctx, taskID)
	if err != nil {
		return nil, err
	}
	return a2a.TruncateHistory(task, historyLength), nil
}

func (s *Store) AppendHistory(ctx context.Context, taskID string, message a2a.Message) error {
	task, eTag, err := s.getTask(ctx, taskID)
	if err != nil {
		return err
	}
	task.History = append(task.History, message)
	if err := s.putTask(ctx, task, eTag); err != nil {
		return err
	}
	return nil
}

func (s *Store) UpdateStatus(ctx context.Context, taskID string, status a2a.TaskStatus) error {
	task, eTag, err := s.getTask(ctx, taskID)
	if err != nil {
		return err
	}
	after, isUpdated, err := a2a.MutateTaskStatus(task, status)
	if err != nil {
		return err
	}
	if !isUpdated {
		return nil
	}
	if err := s.putTask(ctx, after, eTag); err != nil {
		return err
	}
	return nil
}

func (s *Store) UpdateArtifact(ctx context.Context, taskID string, artifact a2a.Artifact) error {
	task, eTag, err := s.getTask(ctx, taskID)
	if err != nil {
		return err
	}
	after, err := a2a.MutateTaskArtifact(task, artifact)
	if err != nil {
		return err
	}
	if err := s.putTask(ctx, after, eTag); err != nil {
		return err
	}
	return nil
}

func (s *Store) getTaskPushNotification(ctx context.Context, taskID string) (*a2a.TaskPushNotificationConfig, *string, error) {
	resp, err := s.client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(s.bucketName),
		Key:    aws.String(s.pushNotificationConfigKey(taskID)),
	})
	if err != nil {
		var apiErr smithy.APIError
		if ok := errors.As(err, &apiErr); ok && apiErr.ErrorCode() == "NoSuchKey" {
			return nil, nil, a2a.ErrTaskPushNotificationNotConfigured
		}
		return nil, nil, err
	}
	var cfg a2a.TaskPushNotificationConfig
	bs, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, nil, err
	}
	defer resp.Body.Close()
	if err := s.opts.Unmarshaller(bs, &cfg); err != nil {
		return nil, nil, err
	}
	return &cfg, resp.ETag, nil
}

// CreateTaskPushNotification creates a push notification configuration for a task.
func (s *Store) CreateTaskPushNotification(ctx context.Context, cfg *a2a.TaskPushNotificationConfig) error {
	_, eTag, err := s.getTaskPushNotification(ctx, cfg.ID)
	if err != nil && !errors.Is(err, a2a.ErrTaskPushNotificationNotConfigured) {
		return err
	}
	bs, err := s.opts.Marshaler(cfg)
	if err != nil {
		return err
	}
	input := &s3.PutObjectInput{
		Bucket: aws.String(s.bucketName),
		Key:    aws.String(s.pushNotificationConfigKey(cfg.ID)),
		Body:   bytes.NewReader(bs),
	}
	if eTag == nil {
		input.IfNoneMatch = aws.String("*")
	} else {
		input.IfMatch = eTag
	}
	_, err = s.client.PutObject(ctx, input)
	return err
}

// GetTaskPushNotification retrieves the push notification configuration for a task by its ID.
func (s *Store) GetTaskPushNotification(ctx context.Context, taskID string) (*a2a.TaskPushNotificationConfig, error) {
	cfg, _, err := s.getTaskPushNotification(ctx, taskID)
	if err != nil {
		return nil, err
	}
	return cfg, nil
}
