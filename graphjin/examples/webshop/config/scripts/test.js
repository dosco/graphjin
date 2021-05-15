function request(vars) {
  return { id: 2 };
}

function response(json) {
  json["email"] = "...@test.com";
  return json;
}