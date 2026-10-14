// 决策操场：用当前用户自己的令牌直接调用原生 /v1/systemone 中继
// （TypeSafe Jev 决策模型），便于验证渠道配置与问题设计。

import React from 'react';
import {
  Button,
  Card,
  Dropdown,
  Form,
  Header,
  Icon,
  Input,
  Label,
  Message,
  Progress,
  Segment,
} from 'semantic-ui-react';
import { showError } from '../../helpers';
import {
  loadPlaygroundTokens,
  relayPost,
  type PlaygroundToken,
} from './relay';

const MODEL_OPTIONS = ['jev-latest', 'jev-1.13.0', 'jev-preview'].map(
  (value) => ({ key: value, text: value, value })
);
const TYPE_OPTIONS = [
  { key: 'noul', text: '是非（noul）', value: 'noul' },
  { key: 'choice', text: '选择（choice）', value: 'choice' },
  { key: 'score', text: '打分（score）', value: 'score' },
];

interface ChoiceOption {
  option: string;
  description: string;
}

interface DraftQuestion {
  uid: number;
  id: string;
  type: 'noul' | 'choice' | 'score';
  instructions: string;
  noulTrue: string;
  noulFalse: string;
  options: ChoiceOption[];
  levels: string[];
}

interface SystemOneUsage {
  input_tokens?: number;
  output_tokens?: number;
}

let uidSeed = 0;

const newQuestion = (index: number): DraftQuestion => ({
  uid: ++uidSeed,
  id: `q${index + 1}`,
  type: 'noul',
  instructions: '',
  noulTrue: '',
  noulFalse: '',
  options: [{ option: '', description: '' }],
  levels: ['', ''],
});

const questionId = (question: DraftQuestion, index: number) =>
  question.id.trim() || `q${index + 1}`;

const toPercent = (value: number) => Math.round(value * 1000) / 10;

const DecisionPlayground = () => {
  const [tokens, setTokens] = React.useState<PlaygroundToken[]>([]);
  const [tokenId, setTokenId] = React.useState<number | null>(null);
  const [model, setModel] = React.useState('jev-latest');
  const [extraModels, setExtraModels] = React.useState<string[]>([]);
  const [stateText, setStateText] = React.useState('');
  const [questions, setQuestions] = React.useState<DraftQuestion[]>([
    newQuestion(0),
  ]);
  const [submitting, setSubmitting] = React.useState(false);
  const [submitted, setSubmitted] = React.useState<DraftQuestion[]>([]);
  const [answers, setAnswers] = React.useState<Record<string, any> | null>(
    null
  );
  const [usage, setUsage] = React.useState<SystemOneUsage | null>(null);
  const [answerModel, setAnswerModel] = React.useState('');

  React.useEffect(() => {
    (async () => {
      try {
        const tokenRows = await loadPlaygroundTokens();
        setTokens(tokenRows);
        if (tokenRows.length > 0) {
          setTokenId(tokenRows[0].id);
        }
      } catch (error) {
        showError(error);
      }
    })();
  }, []);

  const updateQuestion = (uid: number, patch: Partial<DraftQuestion>) =>
    setQuestions((prev) =>
      prev.map((question) =>
        question.uid === uid ? { ...question, ...patch } : question
      )
    );

  const updateOption = (
    uid: number,
    index: number,
    patch: Partial<ChoiceOption>
  ) =>
    setQuestions((prev) =>
      prev.map((question) =>
        question.uid === uid
          ? {
              ...question,
              options: question.options.map((option, i) =>
                i === index ? { ...option, ...patch } : option
              ),
            }
          : question
      )
    );

  const parseState = (): unknown => {
    const text = stateText.trim();
    if (text.startsWith('{') || text.startsWith('[')) {
      try {
        const parsed = JSON.parse(text);
        if (parsed && typeof parsed === 'object') {
          return parsed;
        }
      } catch {
        // 不合法的 JSON 按原样作为字符串发送
      }
    }
    return text;
  };

  const validate = () => {
    const token = tokens.find((item) => item.id === tokenId);
    if (!token) {
      return '请先选择一个启用中的令牌';
    }
    if (!model.trim() || !stateText.trim()) {
      return '请填写模型与状态（state）';
    }
    if (questions.length === 0) {
      return '至少需要一个决策问题';
    }
    for (let i = 0; i < questions.length; i++) {
      const question = questions[i];
      if (!question.instructions.trim()) {
        return `问题 ${questionId(question, i)}：请填写评估说明（instructions）`;
      }
      if (question.type === 'choice') {
        const names = question.options.map((option) => option.option.trim());
        if (names.some((name) => !name)) {
          return `问题 ${questionId(question, i)}：每个选项都需要选项名`;
        }
        if (new Set(names).size !== names.length) {
          return `问题 ${questionId(question, i)}：选项名不能重复`;
        }
      }
      if (question.type === 'score') {
        const filled = question.levels.map((level) => level.trim());
        if (filled.some((level) => !level)) {
          return `问题 ${questionId(question, i)}：每个等级都需要描述`;
        }
        if (filled.length < 2 || filled.length > 10) {
          return `问题 ${questionId(question, i)}：打分等级需要 2–10 个`;
        }
      }
    }
    return null;
  };

  const buildQuestions = () => {
    const built: Record<string, unknown> = {};
    questions.forEach((question, index) => {
      const item: Record<string, unknown> = {
        type: question.type,
        instructions: question.instructions.trim(),
      };
      if (question.type === 'noul') {
        const yes = question.noulTrue.trim();
        const no = question.noulFalse.trim();
        if (yes || no) {
          item.criteria = { true: yes, false: no };
        }
      } else if (question.type === 'choice') {
        item.criteria = Object.fromEntries(
          question.options.map((option) => [
            option.option.trim(),
            option.description.trim() || null,
          ])
        );
      } else {
        item.criteria = question.levels.map((level) => level.trim());
      }
      built[questionId(question, index)] = item;
    });
    return built;
  };

  const submit = async () => {
    const invalid = validate();
    if (invalid) {
      showError(invalid);
      return;
    }
    const token = tokens.find((item) => item.id === tokenId)!;
    setSubmitting(true);
    setAnswers(null);
    setUsage(null);
    try {
      const payload = await relayPost('/v1/systemone', token.key, {
        model: model.trim(),
        state: parseState(),
        questions: buildQuestions(),
      });
      setSubmitted(questions);
      setAnswers(payload?.answers || {});
      setAnswerModel(payload?.model || '');
      setUsage(payload?.usage || null);
    } catch (error) {
      showError(error);
    } finally {
      setSubmitting(false);
    }
  };

  const renderNoul = (answer: any) => (
    <Progress
      percent={toPercent(answer.noul ?? 0)}
      progress='percent'
      indicating
      label={`是（接近 1）的概率：${toPercent(answer.noul ?? 0)}%`}
    />
  );

  const renderDistribution = (probabilities: Record<string, number>) => {
    const entries = Object.entries(probabilities || {}).sort(
      ([, a], [, b]) => (b as number) - (a as number)
    );
    return entries.map(([key, probability]) => (
      <Progress
        key={key}
        percent={toPercent(Number(probability))}
        progress='percent'
        label={`${key}：${toPercent(Number(probability))}%`}
        size='small'
      />
    ));
  };

  const renderAnswer = (question: DraftQuestion, index: number) => {
    const answer = answers?.[questionId(question, index)];
    if (!answer) {
      return null;
    }
    return (
      <Card key={question.uid} fluid>
        <Card.Content>
          <Card.Header>
            {questionId(question, index)}
            <Label
              style={{ marginLeft: '0.5em' }}
              size='tiny'
              content={answer.type}
            />
          </Card.Header>
          <Card.Description>
            {answer.type === 'noul' && renderNoul(answer)}
            {answer.type === 'choice' && (
              <>
                <p>
                  最优选项：<b>{String(answer.choice ?? '-')}</b>
                  {typeof answer.confidence === 'number'
                    ? `（置信度 ${toPercent(answer.confidence)}%）`
                    : ''}
                </p>
                {renderDistribution(answer.probabilities)}
              </>
            )}
            {answer.type === 'score' && (
              <>
                <p>
                  综合得分：<b>{String(answer.score ?? '-')}</b>
                  {typeof answer.confidence === 'number'
                    ? `（置信度 ${toPercent(answer.confidence)}%）`
                    : ''}
                </p>
                {Object.entries(answer.legend || {})
                  .sort(([a], [b]) => Number(a) - Number(b))
                  .map(([level, description]) => {
                    const probability =
                      Number(answer.probabilities?.[level] ?? 0) * 100;
                    return (
                      <Progress
                        key={level}
                        percent={Math.round(probability * 10) / 10}
                        progress='percent'
                        label={`等级 ${level}（${String(
                          description
                        )}）：${Math.round(probability * 10) / 10}%`}
                        size='small'
                      />
                    );
                  })}
              </>
            )}
          </Card.Description>
        </Card.Content>
      </Card>
    );
  };

  return (
    <div style={{ padding: '1rem' }}>
      <Form loading={submitting}>
        <Form.Group widths='equal'>
          <Form.Field required>
            <label>令牌</label>
            <Dropdown
              selection
              placeholder='选择启用中的令牌'
              value={tokenId ?? ''}
              onChange={(_, { value }) => setTokenId(Number(value))}
              options={tokens.map((token) => ({
                key: token.id,
                text: token.name,
                value: token.id,
              }))}
            />
          </Form.Field>
          <Form.Field required>
            <label>模型（可输入任意 jev 模型名）</label>
            <Dropdown
              search
              selection
              allowAdditions
              noResultsMessage={null}
              value={model}
              onAddItem={(_, { value }) => {
                const added = String(value).trim();
                if (added) {
                  setExtraModels((prev) =>
                    prev.includes(added) ? prev : [...prev, added]
                  );
                }
              }}
              onChange={(_, { value }) => setModel(String(value || ''))}
              options={Array.from(
                new Set([
                  ...MODEL_OPTIONS.map((option) => option.value as string),
                  ...extraModels,
                ])
              ).map((value) => ({ key: value, text: value, value }))}
            />
          </Form.Field>
        </Form.Group>
        <Form.TextArea
          label='状态 state（JSON 对象/数组按结构发送，其余按字符串发送）'
          required
          placeholder='例如：{"month":"2026-10","payout_balance":1200,"tickets_open":3}'
          value={stateText}
          onChange={(_, { value }) => setStateText(String(value || ''))}
        />
        <Header as='h4' dividing>
          决策问题
        </Header>
        {questions.map((question, index) => (
          <Segment key={question.uid}>
            <Form.Group widths='equal'>
              <Form.Input
                label='问题 ID'
                value={question.id}
                placeholder={questionId(question, index)}
                onChange={(_, { value }) =>
                  updateQuestion(question.uid, { id: value })
                }
              />
              <Form.Field required>
                <label>类型</label>
                <Dropdown
                  selection
                  value={question.type}
                  onChange={(_, { value }) =>
                    updateQuestion(question.uid, {
                      type: value as DraftQuestion['type'],
                    })
                  }
                  options={TYPE_OPTIONS}
                />
              </Form.Field>
              <Form.Field>
                <label>&nbsp;</label>
                <Button
                  icon
                  basic
                  color='red'
                  onClick={() =>
                    setQuestions((prev) =>
                      prev.filter((item) => item.uid !== question.uid)
                    )
                  }
                >
                  <Icon name='trash' />
                </Button>
              </Form.Field>
            </Form.Group>
            <Form.TextArea
              label='评估说明 instructions'
              required
              value={question.instructions}
              onChange={(_, { value }) =>
                updateQuestion(question.uid, {
                  instructions: String(value || ''),
                })
              }
            />
            {question.type === 'noul' && (
              <Form.Group widths='equal'>
                <Form.Input
                  label='「是」的含义（可选）'
                  value={question.noulTrue}
                  onChange={(_, { value }) =>
                    updateQuestion(question.uid, { noulTrue: value })
                  }
                />
                <Form.Input
                  label='「否」的含义（可选）'
                  value={question.noulFalse}
                  onChange={(_, { value }) =>
                    updateQuestion(question.uid, { noulFalse: value })
                  }
                />
              </Form.Group>
            )}
            {question.type === 'choice' && (
              <>
                <Header as='h5'>选项 criteria（≤255 个）</Header>
                {question.options.map((option, optionIndex) => (
                  <Form.Group key={optionIndex} widths='equal'>
                    <Form.Input
                      label='选项名'
                      value={option.option}
                      onChange={(_, { value }) =>
                        updateOption(question.uid, optionIndex, {
                          option: value,
                        })
                      }
                    />
                    <Form.Input
                      label='评分说明（可空）'
                      value={option.description}
                      onChange={(_, { value }) =>
                        updateOption(question.uid, optionIndex, {
                          description: value,
                        })
                      }
                    />
                    <Form.Field>
                      <label>&nbsp;</label>
                      <Button
                        icon
                        basic
                        onClick={() =>
                          updateQuestion(question.uid, {
                            options: question.options.filter(
                              (_, i) => i !== optionIndex
                            ),
                          })
                        }
                      >
                        <Icon name='minus' />
                      </Button>
                    </Form.Field>
                  </Form.Group>
                ))}
                <Button
                  size='small'
                  onClick={() =>
                    updateQuestion(question.uid, {
                      options: [
                        ...question.options,
                        { option: '', description: '' },
                      ],
                    })
                  }
                >
                  <Icon name='plus' /> 加选项
                </Button>
              </>
            )}
            {question.type === 'score' && (
              <>
                <Header as='h5'>等级描述 criteria（从低到高，2–10 个）</Header>
                {question.levels.map((level, levelIndex) => (
                  <Form.Group key={levelIndex} widths='equal'>
                    <Form.Input
                      label={`等级 ${levelIndex}`}
                      value={level}
                      onChange={(_, { value }) =>
                        updateQuestion(question.uid, {
                          levels: question.levels.map((item, i) =>
                            i === levelIndex ? value : item
                          ),
                        })
                      }
                    />
                    <Form.Field>
                      <label>&nbsp;</label>
                      <Button
                        icon
                        basic
                        onClick={() =>
                          updateQuestion(question.uid, {
                            levels: question.levels.filter(
                              (_, i) => i !== levelIndex
                            ),
                          })
                        }
                      >
                        <Icon name='minus' />
                      </Button>
                    </Form.Field>
                  </Form.Group>
                ))}
                <Button
                  size='small'
                  disabled={question.levels.length >= 10}
                  onClick={() =>
                    updateQuestion(question.uid, {
                      levels: [...question.levels, ''],
                    })
                  }
                >
                  <Icon name='plus' /> 加等级
                </Button>
              </>
            )}
          </Segment>
        ))}
        <Button
          onClick={() =>
            setQuestions((prev) => [...prev, newQuestion(prev.length)])
          }
        >
          <Icon name='plus' /> 加问题
        </Button>
        <Button primary onClick={submit} disabled={submitting}>
          获取决策
        </Button>
      </Form>
      {usage && (
        <Message
          info
          style={{ marginTop: '1rem' }}
          content={`模型 ${answerModel} · 用量：输入 ${
            usage.input_tokens ?? '-'
          } tokens / 输出 ${usage.output_tokens ?? '-'} tokens（输出不计费；实际扣费以日志页为准）`}
        />
      )}
      {answers && submitted.map(renderAnswer)}
      {answers && (
        <details style={{ marginTop: '1rem' }}>
          <summary>原始 JSON</summary>
          <pre style={{ whiteSpace: 'pre-wrap' }}>
            {JSON.stringify({ model: answerModel, answers, usage }, null, 2)}
          </pre>
        </details>
      )}
    </div>
  );
};

export default DecisionPlayground;
