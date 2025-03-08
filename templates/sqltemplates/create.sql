/*
--sqlite dont support this
CREATE DATABASE cards;
USE cards;
*/

CREATE TABLE reader (
	id INTEGER PRIMARY KEY not NULL UNIQUE,
	apiKey VARCHAR(255) not NULL,
	addCard BOOL not NULL,
	writeCard BOOL not NULL
);

CREATE TABLE people (
	id INTEGER PRIMARY KEY not NULL UNIQUE,
	authtoken VARCHAR(255) not NULL,
	name VARCHAR(255) not NULL,
	permission VARCHAR(255) not NULL
);

CREATE TABLE cards (
	serialNumber VARCHAR(255) not NULL UNIQUE,
	writeKey VARCHAR(255) not NULL,
	readKey VARCHAR(255) not NULL,
	owner INTEGER not NULL,
	PRIMARY KEY (serialNumber),
	FOREIGN KEY (owner) REFERENCES people(id)
);
CREATE TABLE accessLog (
	id INTEGER PRIMARY KEY not NULL UNIQUE,
	card INTEGER not NULL,
	reader INTEGER not NULL,
	people INTEGER not NULL,
	allowed BOOL not NULL,
	direction CHAR(2) not NULL, 
	comment TEXT,
	FOREIGN KEY (card) REFERENCES cards(id),
	FOREIGN KEY (reader) REFERENCES reader(id),
	FOREIGN KEY (people) REFERENCES people(id)
);

CREATE TABLE admins (
	id INTEGER PRIMARY KEY not NULL UNIQUE,
	username VARCHAR(255) not NULL UNIQUE,
	pwhash TEXT not NULL --idq the type right now
);
