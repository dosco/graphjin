function main(input) {
  const appointments = input.appointments || [];
  const rooms = input.rooms || [];

  const perRoom = {};
  appointments.forEach((appt) => {
    const key = String(appt.room_id);
    perRoom[key] = (perRoom[key] || 0) + 1;
  });

  const overbooked = Object.keys(perRoom).filter((room) => perRoom[room] > 8);

  return {
    rooms_checked: rooms.length,
    appointments_checked: appointments.length,
    overbooked_rooms: overbooked,
    recommendation:
      overbooked.length > 0
        ? "overbook risk: rooms " + overbooked.join(", ") + " exceed 8 daily appointments"
        : "capacity ok across rooms",
  };
}
