-- LowLex schema, version 3: full-text search.
-- One row per key (rowid = key id) with its name, description and every text, split into
-- trigrams so any substring of 3+ characters can be found without scanning every text.
-- Writes only mark the key in search_dirty (with WHERE NOT EXISTS: inside a trigger, OR IGNORE
-- would take the outer statement's conflict policy and fail during upserts); the server folds dirty keys into the index in
-- batches, so imports stay fast.

CREATE VIRTUAL TABLE search_index USING fts5(text, content='', contentless_delete=1, tokenize='trigram', detail='none');

CREATE TABLE search_dirty (key_id INTEGER PRIMARY KEY);

CREATE TRIGGER search_translation_insert AFTER INSERT ON translations BEGIN
  INSERT INTO search_dirty (key_id) SELECT new.key_id WHERE NOT EXISTS (SELECT 1 FROM search_dirty WHERE key_id = new.key_id);
END;
CREATE TRIGGER search_translation_update AFTER UPDATE OF text ON translations BEGIN
  INSERT INTO search_dirty (key_id) SELECT new.key_id WHERE NOT EXISTS (SELECT 1 FROM search_dirty WHERE key_id = new.key_id);
END;
CREATE TRIGGER search_translation_delete AFTER DELETE ON translations BEGIN
  INSERT INTO search_dirty (key_id) SELECT old.key_id WHERE NOT EXISTS (SELECT 1 FROM search_dirty WHERE key_id = old.key_id);
END;
CREATE TRIGGER search_key_insert AFTER INSERT ON keys BEGIN
  INSERT INTO search_dirty (key_id) SELECT new.id WHERE NOT EXISTS (SELECT 1 FROM search_dirty WHERE key_id = new.id);
END;
CREATE TRIGGER search_key_update AFTER UPDATE OF name, description ON keys BEGIN
  INSERT INTO search_dirty (key_id) SELECT new.id WHERE NOT EXISTS (SELECT 1 FROM search_dirty WHERE key_id = new.id);
END;
CREATE TRIGGER search_key_delete AFTER DELETE ON keys BEGIN
  INSERT INTO search_dirty (key_id) SELECT old.id WHERE NOT EXISTS (SELECT 1 FROM search_dirty WHERE key_id = old.id);
END;

-- existing keys are indexed on the first start after the upgrade
INSERT INTO search_dirty (key_id) SELECT id FROM keys;
