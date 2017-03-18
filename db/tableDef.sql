CREATE TABLE IF NOT EXISTS timeline (
       /* Year */
       annum INTEGER NOT NULL PRIMARY KEY,
       timestamp INTEGER
);

CREATE TABLE IF NOT EXISTS entry (
       /* In unix seconds @ 00:00 */
       timestamp INTEGER NOT NULL PRIMARY KEY,
       title     TEXT,
       next      INTEGER,
       previous  INTEGER,
       /* Content seperated by \n */
       paragraph TEXT,
       /* Content seperate by \n */
       image TEXT
);
