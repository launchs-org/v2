package collector

import (
	"context"
	"time"

	"launchs/shared/model"
)

// linePipeline は、Follower が読んだ行を、取り除き(保存済み)・整え・制限し・まとめて保存する。接続1回ごとに作る。
type linePipeline struct {
	follower *Follower
	ctx      context.Context
	filter   *model.LogResumeFilter
	limiter  *ingestLimiter
	batch    []model.DeployLog
	// position は、batch の最後まで(捨てた行も含む)の、新しい読み取り位置。
	position model.ResumePosition
	// dirty は、保存していない変化(行、または、位置の進み)があるか。
	dirty bool
	// lastNS は、直前に見た行の時刻(時刻を読めない行や、印の行に使う)。
	lastNS int64
}

// newLinePipeline は、linePipeline を作る。読み取り位置は、Follower が持つ、保存済みの位置から始める。
//
// 引数:
//   - follower: 行を受け取る Follower。
//   - ctx: 止める合図(書くときの context に使う)。
//
// 戻り値:
//   - 作成した linePipeline。
func newLinePipeline(follower *Follower, ctx context.Context) *linePipeline {
	// 保存済みの位置から、フィルターと制限を作る。
	return &linePipeline{
		follower: follower, ctx: ctx, filter: model.NewLogResumeFilter(follower.position),
		limiter: newIngestLimiter(follower.settings.IngestBytesPerSecond), position: follower.position, lastNS: follower.position.NS,
	}
}

// add は、読んだ1行を処理する。新しい行なら、整えて、速さの上限の範囲で、バッチに入れる。バッチが一杯なら、保存する。
//
// 引数:
//   - line: 読んだ行。
//
// 戻り値:
//   - 保存に失敗したときのエラー。
func (pipeline *linePipeline) add(line rawLine) error {
	// 時刻を読めない行は、直前の時刻で保存する(位置は進めない。通常は、起こらない)。
	nanoseconds := line.ns
	if line.hasTimestamp {
		// すでに保存した行は、捨てる。新しい行なら、位置が進む。
		if !pipeline.filter.Accept(nanoseconds) {
			return nil
		}
		pipeline.position = pipeline.filter.Position()
		pipeline.lastNS = nanoseconds
	} else {
		nanoseconds = pipeline.lastNS
	}
	pipeline.dirty = true
	// 整えて、速さの上限を確かめる(超えたら、捨てる。捨てた行の分も、位置は進む)。
	text := model.SanitizeLogLine(line.text, pipeline.follower.settings.MaxLineBytes)
	allowed, droppedBefore := pipeline.limiter.allow(len(text), pipeline.follower.settings.Now())
	if droppedBefore > 0 {
		pipeline.batch = append(pipeline.batch, model.DeployLog{LoggedNS: nanoseconds, Line: droppedMarker(droppedBefore)})
	}
	if allowed {
		pipeline.batch = append(pipeline.batch, model.DeployLog{LoggedNS: nanoseconds, Line: text})
	}
	// バッチが一杯なら、すぐ保存する。
	if len(pipeline.batch) >= pipeline.follower.settings.BatchLines {
		return pipeline.flush(false)
	}
	return nil
}

// flush は、バッチと読み取り位置を、同じトランザクションで保存する。
//
// 引数:
//   - final: true なら、これで最後(捨てた行の数の印も、書く。止める合図の後でも書けるよう、context を切り離す)。
//
// 戻り値:
//   - 保存に失敗したときのエラー(このとき、バッチは残り、位置も進まない)。
func (pipeline *linePipeline) flush(final bool) error {
	// 最後なら、まだ知らせていない、捨てた行の数を、印にする。
	if final {
		if dropped := pipeline.limiter.takeDropped(); dropped > 0 {
			pipeline.batch = append(pipeline.batch, model.DeployLog{LoggedNS: pipeline.lastNS, Line: droppedMarker(dropped)})
			pipeline.dirty = true
		}
	}
	// 保存するものが無ければ、何もしない。
	if !pipeline.dirty {
		return nil
	}
	// 書く(最後は、止める合図の後でも書けるよう、切り離して、時間を限る)。
	writeContext := pipeline.ctx
	if final {
		var cancel context.CancelFunc
		writeContext, cancel = context.WithTimeout(context.WithoutCancel(pipeline.ctx), 5*time.Second)
		defer cancel()
	}
	if appendError := pipeline.follower.store.AppendLines(writeContext, pipeline.follower.podUID, pipeline.batch, pipeline.position); appendError != nil {
		return appendError
	}
	// 保存できたので、Follower の位置を進めて、バッチを空にする。
	pipeline.follower.position = pipeline.position
	pipeline.batch = nil
	pipeline.dirty = false
	return nil
}
