package login

// LoginPartial matches the partial-login contract used by meta/admin responses.
// In the Go model, Login already allows empty platform/user and zero-value status.
type LoginPartial = Login
