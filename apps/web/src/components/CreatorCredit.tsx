/**
 * Always-on creator attribution. Fixed to the bottom-right of the
 * viewport above the input bar. Kept out of the AppHeader so the brand
 * link and the creator link don't sit on top of each other.
 */

const CREATOR_URL = "https://openneko.app/#about";
const CREATOR_NAME = "Amit Deshmukh";

export default function CreatorCredit() {
  return (
    <a
      href={CREATOR_URL}
      target="_blank"
      rel="noreferrer"
      className="creator-credit"
    >
      Built by {CREATOR_NAME} ↗
    </a>
  );
}
