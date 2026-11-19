-- テンプレートから作った実行に、使ったテンプレートの版(x.y.z)を記録する。版を扱う前の記録は、空文字。
ALTER TABLE template_applies ADD COLUMN template_version text NOT NULL DEFAULT '';
