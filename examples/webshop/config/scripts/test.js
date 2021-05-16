// example script

function request(vars) {
  return { id: 2 };
}

function response(json) {
  json["email"] = "u..@test.com";
  return json;
}