export function AdminError({ message }: { message: string | null }) {
  if (!message) return null;
  return (
    <div className="mb-4 rounded-control bg-danger-soft px-3 py-2 text-sm text-danger" role="alert">
      {message}
    </div>
  );
}
