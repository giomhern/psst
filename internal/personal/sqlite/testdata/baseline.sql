-- Entirely synthetic: this is a minimal test schema, not a macOS schema dump.
CREATE TABLE chat (ROWID INTEGER PRIMARY KEY, guid TEXT, display_name TEXT);
CREATE TABLE message (
    ROWID INTEGER PRIMARY KEY,
    guid TEXT,
    text TEXT,
    date INTEGER,
    is_from_me INTEGER,
    handle_id INTEGER
);
CREATE TABLE handle (ROWID INTEGER PRIMARY KEY, id TEXT);
CREATE TABLE chat_message_join (chat_id INTEGER, message_id INTEGER);
CREATE TABLE chat_handle_join (chat_id INTEGER, handle_id INTEGER);
INSERT INTO message (guid, text, date, is_from_me, handle_id)
VALUES ('synthetic-guid', 'synthetic-private-message', 0, 0, 1);
