-- Rename only the unchanged built-in project; preserve operator-authored names.
UPDATE projects SET name = 'Deck', slug = 'deck'
WHERE id = 'prj_default' AND name = 'AppDeck' AND slug = 'appdeck';
