"use client";

import { Button } from "@/components/ui/button";
import { BotAvatar } from "./BotAvatar";
import { useThreadBot } from "./useThreadBot";

/** The first screen of a bot's chat. A first job fills the composer for the operator to refine. */
export default function BotGreeting({
  threadId,
  onPick,
}: {
  threadId: string | null;
  onPick: (text: string) => void;
}) {
  const { bot, starters } = useThreadBot(threadId);
  if (!bot) return null;

  return (
    <div className="bot-greeting">
      <BotAvatar coat={bot.coat} pattern={bot.pattern} size={96} />
      <p className="bot-greeting-hello">Hi, I&apos;m {bot.name}.</p>
      {starters.length ? (
        <div className="bot-greeting-card">
          <h2 className="bot-greeting-question">What should I help with first?</h2>
          <div className="bot-greeting-options">
            {starters.map((job, index) => (
              <Button key={job} variant="secondary" className="bot-greeting-option" onClick={() => onPick(job)}>
                <span className="bot-greeting-letter" aria-hidden="true">
                  {String.fromCharCode(65 + index)}
                </span>
                <span>{job}</span>
              </Button>
            ))}
          </div>
          <p className="bot-greeting-hint">Pick one to edit it below, or tell me in your own words.</p>
        </div>
      ) : (
        <p className="bot-greeting-hint">
          {bot.instructions ? bot.instructions : "Tell me what to help with in the chat below."}
        </p>
      )}
    </div>
  );
}
