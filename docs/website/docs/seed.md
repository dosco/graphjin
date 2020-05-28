---
id: seed
title: Database Seeding
sidebar_label: Seed Scripts
---

While developing it's often useful to be able to have fake data available in the database. Fake data can help with building the UI and save you time when trying to get the GraphQL query correct. Super Graph has the ability do this for you. All you have to do is write a seed script `config/seed.js` (In Javascript) and use the `db:seed` command line option. Below is an example of kind of things you can do in a seed script.

## Creating fake users

Since all mutations and queries are in standard GraphQL you can use all the features available in Super Graph GraphQL.

```javascript
var users = [];

for (i = 0; i < 20; i++) {
  var data = {
    slug: util.make_slug(fake.first_name() + "-" + fake.last_name()),
    first_name: fake.first_name(),
    last_name: fake.last_name(),
    picture_url: fake.avatar_url(),
    email: fake.email(),
    bio: fake.sentence(10),
  };

  var res = graphql(" \
	mutation { \
		user(insert: $data) { \
			id \
		} \
	}", { data: data });

  users.push(res.user);
}
```

## Inserting the users fake blog posts

Another example highlighting how the `connect` syntax of Super Graph GraphQL can be used to connect inserted posts
to random users that were previously created. For futher details checkout the [seed script](/seed) documentation.

```javascript
var posts = [];

for (i = 0; i < 1500; i++) {
  var user.id = users[Math.floor(Math.random() * 10)];

  var data = {
    slug:       util.make_slug(fake.sentence(3) + i),
    body:       fake.sentence(100),
    published:   true,
    thread: {
      connect: { user: user.id }
    }
  }

  var res = graphql(" \
  mutation { \
    post(insert: $data) { \
      id \
    } \
  }",
  { data: data },
  { user_id: u.id })

  posts.push(res.post.slug)
}
```

## Insert a large number of rows efficiently

This feature uses the `COPY` functionality available in Postgres this is the best way to
insert a large number of rows into a table. The `import_csv` function reads in a CSV file using the first
line of the file as column names.

```javascript
import_csv("post_tags", "./tags.csv");
```

## A list of fake data functions available to you.

```
person
name
name_prefix
name_suffix
first_name
last_name
gender
ssn
contact
email
phone
phone_formatted
username
password

// Address
address
city
country
country_abr
state
state_abr
street
street_name
street_number
street_prefix
street_suffix
zip
latitude
latitude_in_range
longitude
longitude_in_range

// Beer
beer_alcohol
beer_hop
beer_ibu
beer_blg
beer_malt
beer_name
beer_style
beer_yeast

// Cars
car
car_type
car_maker
car_model

// Text
word
sentence
paragraph
question
quote

// Misc
generate
boolean
uuid

// Colors
color
hex_color
rgb_color
safe_color

// Internet
url
image_url
avatar_url
domain_name
domain_suffix
ipv4_address
ipv6_address
http_method
user_agent
user_agent_firefox
user_agent_chrome
user_agent_opera
user_agent_safari

// Date / Time
date
date_range
nano_second
second
minute
hour
month
day
weekday
year
timezone
timezone_abv
timezone_full
timezone_offset

// Payment
price
credit_card
credit_card_cvv
credit_card_number
credit_card_type
currency
currency_long
currency_short

// Company
bs
buzzword
company
company_suffix
job
job_description
job_level
job_title

// Hacker
hacker_abbreviation
hacker_adjective
hacker_noun
hacker_phrase
hacker_verb

//Hipster
hipster_word
hipster_paragraph
hipster_sentence

// File
file_extension
file_mine_type

// Numbers
number
numerify
int8
int16
int32
int64
uint8
uint16
uint32
uint64
float32
float32_range
float64
float64_range
shuffle_ints
mac_address

// String
digit
letter
lexify
rand_string
numerify
```

## Some more utility functions

```
shuffle_strings(string_array)
make_slug(text)
make_slug_lang(text, lang)
```
