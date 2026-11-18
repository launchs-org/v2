package controller

import (
	"strings"
	"time"

	"launchs/shared/jst"
	"launchs/shared/model"
)

// LogPodResponse は、ログのある Pod 1つ(deploy の詳細の logPods の要素)。
type LogPodResponse struct {
	// PodID は、Pod の ID("pod-" + k8s の Pod の UID)。
	PodID string `json:"podId"`

	// Running は、Pod が稼働中(収集が続いている)か。
	Running bool `json:"running"`

	// StartedAt は、Pod の作成時刻(JST)。
	StartedAt string `json:"startedAt"`

	// EndedAt は、収集が終わった時刻(JST)。稼働中は null。
	EndedAt *string `json:"endedAt"`
}

// newLogPodResponses は、ストリームの一覧を、logPods のレスポンスにする。
//
// 引数:
//   - streams: ログのあるストリーム(新しい Pod の順)。
//
// 戻り値:
//   - レスポンスの要素(無ければ、空の配列。null にしない)。
func newLogPodResponses(streams []model.DeployLogStream) []LogPodResponse {
	// 1つずつ、時刻を JST の文字列にして、並べる。
	responses := make([]LogPodResponse, 0, len(streams))
	for _, stream := range streams {
		response := LogPodResponse{PodID: podIDPrefix + stream.PodUID, Running: stream.FinishedAt == nil, StartedAt: jst.Format(stream.StartedAt)}
		if stream.FinishedAt != nil {
			endedAt := jst.Format(*stream.FinishedAt)
			response.EndedAt = &endedAt
		}
		responses = append(responses, response)
	}
	return responses
}

// DeployLogsResponse は、ログの取得のレスポンス。
type DeployLogsResponse struct {
	// Logs は、ログの行をつなげた文字列。各行は「<JST の時刻(ミリ秒)> <本文>」+ 改行。行が無ければ空文字。
	Logs string `json:"logs"`

	// NextSince は、次に since に渡す値。
	NextSince int64 `json:"nextSince"`
}

// newDeployLogsResponse は、ログの行を、レスポンスの文字列にする。
//
// 引数:
//   - lines: ログの行(id の順)。
//   - nextSince: 次に since に渡す値。
//
// 戻り値:
//   - レスポンス。
func newDeployLogsResponse(lines []model.DeployLog, nextSince int64) DeployLogsResponse {
	// 各行の先頭に、行の時刻(JST、ミリ秒)を付けて、つなげる。
	var builder strings.Builder
	for _, line := range lines {
		builder.WriteString(jst.FormatMillis(time.Unix(0, line.LoggedNS)))
		builder.WriteByte(' ')
		builder.WriteString(line.Line)
		builder.WriteByte('\n')
	}
	return DeployLogsResponse{Logs: builder.String(), NextSince: nextSince}
}
