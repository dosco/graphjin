import { createMcpServer, defineMcpTool } from "../mcp-server";
import type {
  AgentEvent,
  AgentInputQuestion,
  AgentSurfaceMessage,
} from "../agent-backend";
import { z } from "zod";
import { A2UI_CATALOG_ID, A2UI_VERSION } from "./a2ui-contract";

export const ASK_USER_SERVER_NAME = "neko_interaction" as const;
export const ASK_USER_TOOL_NAME = "ask_user_question" as const;
export const ASK_USER_TOOL_TITLE =
  "mcp_neko_interaction_ask_user_question" as const;

const optionSchema = z
  .object({
    label: z.string().trim().min(1).max(80),
    description: z.string().trim().min(1).max(240).optional(),
  })
  .strict();

const questionSchema = z
  .object({
    header: z.string().trim().min(1).max(40).optional(),
    question: z.string().trim().min(1).max(500),
    options: z.array(optionSchema).min(2).max(5).optional(),
  })
  .strict();

export const ASK_USER_INPUT_SHAPE = {
  reason: z.string().trim().min(1).max(500).optional(),
  questions: z.array(questionSchema).min(1).max(3),
};

export type AskUserQuestionArgs = z.infer<
  z.ZodObject<typeof ASK_USER_INPUT_SHAPE>
>;

function normalizedQuestions(
  questions: AskUserQuestionArgs["questions"],
): AgentInputQuestion[] {
  return questions.map((question, index) => ({
    id: `q${index + 1}`,
    ...(question.header ? { header: question.header } : {}),
    question: question.question,
    ...(question.options ? { options: question.options } : {}),
  }));
}

/**
 * Build the deterministic clarification form. The model supplies meaning; the
 * host owns protocol shape and the submit action, so asking a question cannot
 * degrade into malformed or non-submittable generated UI.
 */
export function clarificationSurface(
  runId: string,
  questions: AgentInputQuestion[],
  reason?: string,
): AgentSurfaceMessage[] {
  const surfaceId = `clarification-${runId}`;
  const components: Array<Record<string, unknown>> = [];
  const children: string[] = [];
  const answers: Record<string, unknown> = {};
  const requirements: Array<{ path: string }> = [];

  for (const question of questions) {
    const inputId = `${question.id}-input`;
    children.push(inputId);
    if (question.options?.length) {
      answers[question.id] = [];
      requirements.push({ path: `/answers/${question.id}/0` });
      components.push({
        id: inputId,
        component: "ChoicePicker",
        label: question.header
          ? `${question.header}: ${question.question}`
          : question.question,
        options: question.options.map((option) => ({
          label: option.description
            ? `${option.label} — ${option.description}`
            : option.label,
          value: option.label,
        })),
        value: { path: `/answers/${question.id}` },
        variant: "mutuallyExclusive",
      });
    } else {
      answers[question.id] = "";
      requirements.push({ path: `/answers/${question.id}` });
      components.push({
        id: inputId,
        component: "TextField",
        label: question.header
          ? `${question.header}: ${question.question}`
          : question.question,
        value: { path: `/answers/${question.id}` },
        variant: "longText",
      });
    }
  }

  children.push("submit");
  components.unshift({
    id: "root",
    component: "Answer",
    eyebrow: "Clarification",
    title:
      questions.length === 1
        ? "One detail before I continue"
        : "A few details before I continue",
    ...(reason ? { subtitle: reason } : {}),
    children,
  });
  components.push(
    { id: "submit-label", component: "Text", text: "Continue", variant: "body" },
    {
      id: "submit",
      component: "Button",
      child: "submit-label",
      variant: "primary",
      requires: requirements,
      action: {
        event: {
          name: "submit_clarification",
          context: {
            prompt:
              "Continue the previous request using these operator-supplied answers. Re-check any remaining external facts and do not infer omitted values.",
            questions: questions.map(({ id, header, question }) => ({
              id,
              ...(header ? { header } : {}),
              question,
            })),
            answers: { path: "/answers" },
          },
        },
      },
    },
  );

  return [
    {
      version: A2UI_VERSION,
      createSurface: {
        surfaceId,
        catalogId: A2UI_CATALOG_ID,
        components,
        dataModel: { answers },
      },
    },
  ];
}

export function buildAskUserQuestionServer(opts: {
  runId: string;
  wantsCards: boolean;
  emit: (event: AgentEvent) => Promise<void> | void;
}) {
  const askUserQuestion = defineMcpTool(
    ASK_USER_TOOL_NAME,
    [
      "Pause this Work-chat turn and ask the operator for missing information.",
      "Use only when uncertainty is material to correctness, scope, cost, timing, authorization, or an irreversible decision and available tools cannot resolve it.",
      "Ask one to three focused questions. After this call, stop: do not render an answer, provide totals or recommendations, emit vitals/follow-ups, or call another tool.",
    ].join(" "),
    ASK_USER_INPUT_SHAPE,
    async (args: AskUserQuestionArgs) => {
      const questions = normalizedQuestions(args.questions);
      const surfaceId = `clarification-${opts.runId}`;
      if (opts.wantsCards) {
        await opts.emit({
          type: "surface",
          messages: clarificationSurface(opts.runId, questions, args.reason),
        });
      }
      await opts.emit({
        type: "needs_input",
        question:
          questions.length === 1
            ? questions[0].question
            : `I need ${questions.length} details before I can continue.`,
        ...(questions.length === 1 && questions[0].options
          ? { options: questions[0].options.map((option) => option.label) }
          : {}),
        questions,
        ...(args.reason ? { reason: args.reason } : {}),
        ...(opts.wantsCards ? { surfaceId } : {}),
      });
      return {
        content: [
          {
            type: "text" as const,
            text: JSON.stringify({
              ok: true,
              status: "needs_input",
              instruction:
                "The clarification was presented to the operator. End this turn now without any further tools, answer content, vitals, or follow-ups.",
            }),
          },
        ],
      };
    },
  );

  return createMcpServer({
    name: ASK_USER_SERVER_NAME,
    version: "1.0.0",
    tools: [askUserQuestion],
  });
}
