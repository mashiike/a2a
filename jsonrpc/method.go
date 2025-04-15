package jsonrpc

// Define constants for methods
const (
	MethodTasksSend                = "tasks/send"
	MethodTasksGet                 = "tasks/get"
	MethodTasksCancel              = "tasks/cancel"
	MethodTasksPushNotificationSet = "tasks/pushNotification/set"
	MethodTasksPushNotificationGet = "tasks/pushNotification/get"
	MethodTasksResubscribe         = "tasks/resubscribe"
	MethodTasksSendSubscribe       = "tasks/sendSubscribe"
)

func Methods() []string {
	return []string{
		MethodTasksSend,
		MethodTasksGet,
		MethodTasksCancel,
		MethodTasksPushNotificationSet,
		MethodTasksPushNotificationGet,
		MethodTasksResubscribe,
		MethodTasksSendSubscribe,
	}
}
