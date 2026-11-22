-- deploy のイメージを、あとから更新できるようにする(まだ適用しないイメージ。変更が無ければ NULL)。
-- 更新を受け付けると、起動設定(env、command、args)の pending も一緒に作る。適用(apply)は、起動設定と同じ流れ。
ALTER TABLE deploys ADD COLUMN pending_image_ref text;
