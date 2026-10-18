-- Fictional chats only. IDs/dates deliberately test local-row ordering.
INSERT INTO chat (ROWID, guid, display_name) VALUES
(1, 'synthetic-group', 'Study group'),
(2, 'synthetic-direct', NULL),
(3, 'synthetic-unicode', '🐈 日本語'),
(4, NULL, NULL),
(5, 'synthetic-empty', '   '),
(6, 'synthetic-name', '  名稱  ');
INSERT INTO handle (ROWID, id) VALUES (1, 'synthetic-a'), (2, 'synthetic-b');
INSERT INTO chat_handle_join VALUES (1, 1), (1, 2);
INSERT INTO message (ROWID, guid, text, date) VALUES
(10, 'synthetic-10', 'private fixture body', 999999999),
(20, 'synthetic-20', NULL, 1);
INSERT INTO chat_message_join VALUES (1, 10), (1, 10), (2, 20), (3, 20), (4, 9999);
